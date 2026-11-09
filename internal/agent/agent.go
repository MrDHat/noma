package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MrDHat/noma/internal/api"
)

// Agent sets up and runs one workspace.
type Agent struct {
	Client *Client
	// Dir is the workspace volume.
	Dir string
	// ClaudeBin is the claude binary.
	ClaudeBin string

	// codes passes sealed sign-in codes from the message loop to signIn.
	codes chan *api.SignInCode
	// vault holds the workspace's credentials, in memory only.
	vault vault
	// credentialsChanged passes the server's change message to
	// keepCredentials.
	credentialsChanged chan struct{}
	// restartClaude asks for Claude to restart with new credentials.
	restartClaude chan struct{}
}

// errNewCredentials ends a Claude run so it restarts with new credentials.
var errNewCredentials = errors.New("credentials changed")

// Run sets the workspace up, starts Claude and keeps it running, and serves
// server messages until ctx is done. A failed setup is reported as an error
// phase; the agent keeps running so the pod doesn't crash-loop.
func (a *Agent) Run(ctx context.Context) error {
	cfg, err := retry(ctx, func() (*api.AgentConfig, error) { return a.Client.Config(ctx) })
	if err != nil {
		return err
	}
	slog.Info("workspace", "id", cfg.ID, "name", cfg.Name)
	a.codes = make(chan *api.SignInCode, 4)
	a.credentialsChanged = make(chan struct{}, 1)
	a.restartClaude = make(chan struct{}, 1)
	// Before the clone, which may need a git token.
	if _, err := retry(ctx, func() (struct{}, error) { return struct{}{}, a.loadCredentials(ctx) }); err != nil {
		return err
	}
	if err := a.serveCredentials(ctx); err != nil {
		return fmt.Errorf("serve credentials: %w", err)
	}
	go a.keepCredentials(ctx)

	go func() {
		if err := a.setup(ctx, cfg); err != nil {
			slog.Error("setup failed", "err", err)
			a.report(ctx, api.AgentStatus{Phase: api.PhaseError, Message: err.Error()})
			return
		}
		a.superviseClaude(ctx, &Claude{
			Bin:  a.ClaudeBin,
			Home: os.Getenv("HOME"),
			Dir:  filepath.Join(a.Dir, repoDir(cfg)),
			Name: cfg.Name,
			Env:  a.vault.env,
		})
	}()
	return a.serve(ctx)
}

// superviseClaude waits for a Claude login, then runs Remote Control and
// restarts it whenever it exits.
func (a *Agent) superviseClaude(ctx context.Context, c *Claude) {
	a.report(ctx, api.AgentStatus{Phase: api.PhaseClaude})
	if err := c.Seed(); err != nil {
		a.report(ctx, api.AgentStatus{Phase: api.PhaseError, Message: "configure Claude: " + err.Error()})
		return
	}
	for wait := time.Second; ctx.Err() == nil; {
		if !a.waitForLogin(ctx, c) {
			return
		}
		// This run starts with the current credentials.
		select {
		case <-a.restartClaude:
		default:
		}
		run, stopRun := context.WithCancelCause(ctx)
		go func() {
			select {
			case <-a.restartClaude:
				stopRun(errNewCredentials)
			case <-run.Done():
			}
		}()
		start := time.Now()
		err := c.RemoteControl(run, func(link string) {
			slog.Info("remote control ready", "link", link)
			a.report(ctx, api.AgentStatus{Phase: api.PhaseReady, Link: link})
		})
		newCredentials := errors.Is(context.Cause(run), errNewCredentials)
		stopRun(nil)
		if ctx.Err() != nil {
			return
		}
		if newCredentials {
			slog.Info("restarting claude with new credentials")
			a.report(ctx, api.AgentStatus{Phase: api.PhaseClaude, Message: "restarting Claude with new credentials"})
			wait = time.Second
			continue
		}
		slog.Warn("claude stopped, restarting", "err", err)
		a.report(ctx, api.AgentStatus{Phase: api.PhaseClaude, Message: "restarting Claude"})
		if time.Since(start) > time.Minute {
			wait = time.Second
		}
		sleep(ctx, wait)
		wait = min(wait*2, time.Minute)
	}
}

// waitForLogin returns once Claude has a login, running sign-ins until one
// succeeds, or false if ctx ends first.
func (a *Agent) waitForLogin(ctx context.Context, c *Claude) bool {
	for ctx.Err() == nil {
		ok, err := c.LoggedIn(ctx)
		if err != nil {
			slog.Warn("check claude login", "err", err)
		}
		if ok {
			return true
		}
		if err := a.signIn(ctx, c); err != nil && ctx.Err() == nil {
			slog.Warn("claude sign-in failed", "err", err)
			a.report(ctx, api.AgentStatus{Phase: api.PhaseClaude, Message: "sign-in failed, trying again"})
			sleep(ctx, 2*time.Second)
		}
	}
	return false
}

func (a *Agent) setup(ctx context.Context, cfg *api.AgentConfig) error {
	if err := os.MkdirAll(os.Getenv("HOME"), 0o700); err != nil {
		return fmt.Errorf("create home: %w", err)
	}
	if err := useCredentialHelper(ctx); err != nil {
		return fmt.Errorf("set up git credentials: %w", err)
	}
	repo := filepath.Join(a.Dir, repoDir(cfg))
	if _, err := os.Stat(filepath.Join(repo, ".git")); err == nil {
		return nil // cloned before this pod restarted
	}
	a.report(ctx, api.AgentStatus{Phase: api.PhaseCloning, Message: cfg.RepoURL})
	_ = os.RemoveAll(repo) // a clone cut short by a restart
	cmd := exec.CommandContext(ctx, "git", "clone", "--", cfg.RepoURL, repo)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %v: %s", err, lastLine(out))
	}
	return nil
}

// repoDir is the checkout's directory name inside the workspace volume.
func repoDir(cfg *api.AgentConfig) string { return cfg.Name }

// serve long-polls for server messages.
func (a *Agent) serve(ctx context.Context) error {
	for ctx.Err() == nil {
		m, err := a.Client.NextMessage(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("poll failed", "err", err)
				sleep(ctx, 2*time.Second)
			}
			continue
		}
		switch {
		case m == nil:
		case m.Type == api.MessageCredentials:
			select {
			case a.credentialsChanged <- struct{}{}:
			default: // a refresh is already due
			}
		case m.Type == api.MessageSignInCode && m.SignInCode != nil:
			select {
			case a.codes <- m.SignInCode:
			default:
				slog.Warn("dropped a sign-in code nobody is waiting for")
			}
		default:
			slog.Warn("unknown message", "type", m.Type)
		}
	}
	return nil
}

// report sends a status, retrying until the server takes it.
func (a *Agent) report(ctx context.Context, st api.AgentStatus) {
	_, _ = retry(ctx, func() (struct{}, error) { return struct{}{}, a.Client.Status(ctx, st) })
}

// retry calls f until it succeeds or ctx is done, backing off up to 10s.
func retry[T any](ctx context.Context, f func() (T, error)) (T, error) {
	wait := 500 * time.Millisecond
	for {
		v, err := f()
		if err == nil || ctx.Err() != nil {
			return v, err
		}
		slog.Warn("server call failed, retrying", "err", err)
		sleep(ctx, wait)
		wait = min(wait*2, 10*time.Second)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return lines[len(lines)-1]
}
