package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/MrDHat/noma/internal/api"
)

// credentialSocket is where the agent answers git's credential helper. /tmp
// is the pod's in-memory scratch space, not the workspace volume, so no
// token is ever written to disk (M3-13).
var credentialSocket = "/tmp/noma-credentials.sock"

// gitUser is the user name sent with a token; hosts ignore it for tokens.
const gitUser = "x-access-token"

// vault holds the workspace's credentials in memory.
type vault struct {
	mu    sync.Mutex
	creds []api.Credential
}

func (v *vault) set(cs []api.Credential) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.creds = cs
}

// gitToken returns the token for a git host, if there is one.
func (v *vault) gitToken(host string) (string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, c := range v.creds {
		if c.GitHost != "" && strings.EqualFold(c.GitHost, host) {
			return c.Value, true
		}
	}
	return "", false
}

// loadCredentials fetches the workspace's credentials from the server.
func (a *Agent) loadCredentials(ctx context.Context) error {
	list, err := a.Client.Credentials(ctx)
	if err != nil {
		return err
	}
	a.vault.set(list.Credentials)
	slog.Info("credentials loaded", "count", len(list.Credentials))
	return nil
}

// serveCredentials answers credential helper requests on the socket until
// ctx is done. Each request is one line, a host; the answer is git's
// username and password lines, or nothing.
func (a *Agent) serveCredentials(ctx context.Context) error {
	_ = os.Remove(credentialSocket)
	l, err := net.Listen("unix", credentialSocket)
	if err != nil {
		return err
	}
	if err := os.Chmod(credentialSocket, 0o600); err != nil {
		_ = l.Close()
		return err
	}
	go func() { <-ctx.Done(); _ = l.Close() }()
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go a.answerCredential(conn)
		}
	}()
	return nil
}

func (a *Agent) answerCredential(conn net.Conn) {
	defer conn.Close() //nolint:errcheck
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	host, err := bufio.NewReader(io.LimitReader(conn, 512)).ReadString('\n')
	if err != nil {
		return
	}
	if token, ok := a.vault.gitToken(strings.TrimSpace(host)); ok {
		_, _ = fmt.Fprintf(conn, "username=%s\npassword=%s\n", gitUser, token)
	}
}

// useCredentialHelper makes git in the workspace ask this agent for
// credentials. The git config names the helper only; it holds no token.
func useCredentialHelper(ctx context.Context) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "config", "--global", "--replace-all", "credential.helper", exe+" credential")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config: %v: %s", err, lastLine(out))
	}
	return nil
}

// CredentialHelper is git's credential helper, run as `noma-agent
// credential get`. It asks the running agent over its socket. It ignores
// store and erase, so git never saves a token anywhere.
func CredentialHelper(args []string, in io.Reader, out io.Writer) error {
	if len(args) != 1 || args[0] != "get" {
		return nil
	}
	var protocol, host string
	sc := bufio.NewScanner(in)
	for sc.Scan() && sc.Text() != "" {
		k, v, _ := strings.Cut(sc.Text(), "=")
		switch k {
		case "protocol":
			protocol = v
		case "host":
			host = v
		}
	}
	if protocol != "https" || host == "" {
		return nil
	}
	conn, err := net.DialTimeout("unix", credentialSocket, 5*time.Second)
	if err != nil {
		return fmt.Errorf("noma-agent is not answering for credentials: %w", err)
	}
	defer conn.Close() //nolint:errcheck
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintf(conn, "%s\n", host); err != nil {
		return err
	}
	if _, err := io.Copy(out, conn); err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
		return err
	}
	return nil
}
