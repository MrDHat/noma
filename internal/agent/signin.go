package agent

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/creack/pty"
	"golang.org/x/crypto/nacl/box"

	"github.com/MrDHat/noma/internal/api"
)

const (
	// signInTimeout bounds one sign-in attempt; a fresh one starts after it.
	signInTimeout = 15 * time.Minute
	// loginExitTimeout is how long Claude gets to finish after the code.
	loginExitTimeout = 2 * time.Minute
	// sharedLoginCheck is how often a waiting sign-in checks whether
	// another workspace has signed the user in meanwhile.
	sharedLoginCheck = 3 * time.Second
)

var (
	authorizeURL = regexp.MustCompile(`https://claude\.(?:com|ai)/[^\s\x07\x1b]*oauth/authorize\?[^\s\x07\x1b]+`)
	codePrompt   = regexp.MustCompile(`(?i)paste code`)
)

// signIn runs one `claude auth login`. It sends the sign-in URL and a
// one-time public key to the server, waits for the user's code sealed to
// that key, and types it into Claude. Only this process ever sees the code,
// and the key never outlives the attempt. If another of the user's
// workspaces signs in first, it gives up the attempt: the login is shared.
func (a *Agent) signIn(ctx context.Context, c *Claude) error {
	ctx, cancel := context.WithTimeout(ctx, signInTimeout)
	defer cancel()

	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	publicKey := base64.StdEncoding.EncodeToString(pub[:])

	cmd := c.command(ctx, "auth", "login")
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 1000})
	if err != nil {
		return fmt.Errorf("start claude auth login: %w", err)
	}
	defer tty.Close() //nolint:errcheck
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	tail := &tailBuffer{max: 16 << 10}
	prompted := make(chan string, 1)
	go func() {
		w := promptWatcher{tail: tail, prompted: prompted}
		_, _ = io.Copy(&w, tty)
	}()

	var url string
	select {
	case url = <-prompted:
	case err := <-exited:
		return fmt.Errorf("claude auth login exited early: %v: %s", err, tail.lastLine())
	case <-ctx.Done():
		return ctx.Err()
	}
	a.report(ctx, api.AgentStatus{
		Phase:   api.PhaseSignIn,
		Message: "waiting for Claude sign-in",
		SignIn:  &api.SignIn{URL: url, PublicKey: publicKey},
	})

	check := time.NewTicker(sharedLoginCheck)
	defer check.Stop()
	for {
		var m *api.SignInCode
		select {
		case <-check.C:
			// The login is shared by the user's workspaces (M3-5).
			if ok, _ := c.LoggedIn(ctx); ok {
				a.report(ctx, api.AgentStatus{Phase: api.PhaseClaude, Message: "signed in from another workspace"})
				return nil
			}
			continue
		case m = <-a.codes:
		case err := <-exited:
			return fmt.Errorf("claude auth login exited: %v: %s", err, tail.lastLine())
		case <-ctx.Done():
			return errors.New("no sign-in code arrived in time")
		}
		if m.PublicKey != publicKey {
			continue // for an earlier attempt
		}
		code, err := open(m.SealedCode, pub, priv)
		if err != nil {
			slog.Warn("could not open sign-in code", "err", err)
			continue
		}
		if _, err := io.WriteString(tty, code+"\r"); err != nil {
			return err
		}
		break
	}
	a.report(ctx, api.AgentStatus{Phase: api.PhaseClaude, Message: "signing in"})
	select {
	case err := <-exited:
		if err != nil {
			return fmt.Errorf("sign-in failed: %s", tail.lastLine())
		}
		return nil
	case <-time.After(loginExitTimeout):
		return errors.New("claude auth login did not finish")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// open decrypts a code sealed with box.SealAnonymous.
func open(sealed string, pub, priv *[32]byte) (string, error) {
	b, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	code, ok := box.OpenAnonymous(nil, b, pub, priv)
	if !ok {
		return "", errors.New("sealed for another key, or tampered with")
	}
	s := strings.TrimSpace(string(code))
	if s == "" || strings.ContainsAny(s, "\r\n") {
		return "", errors.New("malformed code")
	}
	return s, nil
}

// promptWatcher reports the sign-in URL once Claude asks for the code.
type promptWatcher struct {
	tail     *tailBuffer
	prompted chan<- string
	done     bool
}

func (w *promptWatcher) Write(p []byte) (int, error) {
	w.tail.Write(p)
	if !w.done {
		text := w.tail.raw()
		if url := authorizeURL.FindString(text); url != "" && codePrompt.MatchString(w.tail.text()) {
			w.done = true
			w.prompted <- url
		}
	}
	return len(p), nil
}
