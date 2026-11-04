package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/nacl/box"

	"github.com/MrDHat/noma/internal/api"
)

// signIn walks the user through one Claude sign-in: it opens the URL, asks
// for the code and sends it sealed to the workspace's one-time key, so
// noma-server only ever handles ciphertext.
func signIn(ctx context.Context, c *Client, name string, s *api.SignIn, in *lines, out io.Writer) error {
	key, err := base64.StdEncoding.DecodeString(s.PublicKey)
	if err != nil || len(key) != 32 {
		return errors.New("workspace sent an invalid sign-in key")
	}
	fmt.Fprintf(out, "\nSign in to Claude for this workspace. Opening your browser:\n  %s\n", s.URL)
	openBrowser(s.URL)
	fmt.Fprint(out, "After approving, paste the code here.\nPaste code: ")
	code, err := readCode(ctx, c, name, s.PublicKey, in)
	if errors.Is(err, errSignedInElsewhere) {
		fmt.Fprintln(out, "\nSigned in from another workspace; no code needed.")
		return nil
	}
	if err != nil {
		return err
	}
	if code == "" {
		return errors.New("no code entered")
	}
	sealed, err := box.SealAnonymous(nil, []byte(code), (*[32]byte)(key), rand.Reader)
	if err != nil {
		return err
	}
	return c.SendSignInCode(ctx, name, api.SignInCode{
		PublicKey:  s.PublicKey,
		SealedCode: base64.StdEncoding.EncodeToString(sealed),
	})
}

// errSignedInElsewhere ends a prompt for a code nobody needs any more.
var errSignedInElsewhere = errors.New("signed in from another workspace")

// readCode reads the code the user pastes. The user's workspaces share one
// Claude login, so it stops asking once the workspace no longer waits for
// this sign-in, as when another workspace signed in first.
func readCode(ctx context.Context, c *Client, name, key string, in *lines) (string, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollInterval):
			}
			ws, err := c.Workspace(ctx, name)
			if err == nil && (ws.Phase != api.PhaseSignIn || ws.SignIn == nil || ws.SignIn.PublicKey != key) {
				cancel(errSignedInElsewhere)
				return
			}
		}
	}()
	line, err := in.read(ctx)
	if err != nil && context.Cause(ctx) == errSignedInElsewhere {
		return "", errSignedInElsewhere
	}
	return line, err
}

// lines reads stdin one line at a time. One goroutine does the reading, so
// a prompt given up on doesn't swallow the answer to the next one.
type lines struct {
	once sync.Once
	in   *bufio.Reader
	ch   chan lineOrErr
}

type lineOrErr struct {
	line string
	err  error
}

func newLines(r io.Reader) *lines { return &lines{in: bufio.NewReader(r), ch: make(chan lineOrErr)} }

// read returns the next line, giving up when ctx is done.
func (l *lines) read(ctx context.Context) (string, error) {
	l.once.Do(func() {
		go func() {
			for {
				line, err := l.in.ReadString('\n')
				if errors.Is(err, io.EOF) && line != "" {
					err = nil
				}
				l.ch <- lineOrErr{strings.TrimSpace(line), err}
				if err != nil {
					close(l.ch)
					return
				}
			}
		}()
	})
	select {
	case r, ok := <-l.ch:
		if !ok {
			return "", io.EOF
		}
		return r.line, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// openBrowser opens url in the user's browser, if it can. The URL is
// printed too, so failing quietly is fine.
func openBrowser(url string) {
	if os.Getenv("NOMA_NO_BROWSER") != "" {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	_ = cmd.Start()
}
