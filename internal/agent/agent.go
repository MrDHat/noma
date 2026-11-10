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
	// restarts and lastExit describe Claude's restarts in this pod; every
	// report carries them. Only the setup goroutine reports.
	restarts int
	lastExit string
}

// Claude restarts back off from a second up to a minute. A run shorter
// than quickRun counts as a crash; maxQuickCrashes in a row stop the
// restarts with an error, until the workspace is stopped and started.
const (
	quickRun        = time.Minute
	maxQuickCrashes = 5
)

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
	quick := 0
	for wait := time.Second; ctx.Err() == nil; {
		if !a.waitForLogin(ctx, c) {
			return
		}
		start := time.Now()
		err := c.RemoteControl(ctx, func(link string) {
			slog.Info("remote control ready", "link", link)
			a.report(ctx, api.AgentStatus{Phase: api.PhaseReady, Link: link})
		})
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("claude remote-control exited")
		}
		slog.Warn("claude stopped", "err", err)
		a.lastExit = err.Error()
		if time.Since(start) > quickRun {
			quick, wait = 0, time.Second
		}
		if quick++; quick >= maxQuickCrashes {
			a.report(ctx, api.AgentStatus{Phase: api.PhaseError,
				Message: fmt.Sprintf("Claude crashed %d times in a row; stop and start the workspace to try again", quick)})
			return
		}
		a.restarts++
		a.report(ctx, api.AgentStatus{Phase: api.PhaseClaude, Message: "restarting Claude"})
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
	if err := setGitIdentity(ctx, cfg.Git); err != nil {
		return err
	}
	repo := filepath.Join(a.Dir, repoDir(cfg))
	if _, err := os.Stat(filepath.Join(repo, ".git")); err == nil {
		return nil // cloned before this pod restarted
	}
	a.report(ctx, api.AgentStatus{Phase: api.PhaseCloning, Message: cfg.RepoURL})
	var err error
	for try, wait := 1, 2*time.Second; ; try, wait = try+1, wait*2 {
		if err = clone(ctx, cfg.RepoURL, repo); err == nil || try == cloneTries || ctx.Err() != nil {
			return err
		}
		slog.Warn("clone failed, retrying", "err", err)
		a.report(ctx, api.AgentStatus{Phase: api.PhaseCloning,
			Message: fmt.Sprintf("%s (try %d of %d failed, retrying)", cfg.RepoURL, try, cloneTries)})
		sleep(ctx, wait)
	}
}

// cloneTries is how often setup tries the clone: once, then three retries
// 2, 4 and 8 seconds apart.
const cloneTries = 4

func clone(ctx context.Context, repoURL, dir string) error {
	_ = os.RemoveAll(dir) // a clone cut short by a restart or a failed try
	cmd := exec.CommandContext(ctx, "git", "clone", "--", repoURL, dir)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %v: %s", err, lastLine(out))
	}
	return nil
}

// setGitIdentity writes the owner's git identity to the workspace's global
// git config at every start, so a change on the laptop reaches workspaces
// the next time they start.
func setGitIdentity(ctx context.Context, g *api.GitIdentity) error {
	if g == nil {
		return nil
	}
	for key, value := range map[string]string{"user.name": g.Name, "user.email": g.Email} {
		if out, err := exec.CommandContext(ctx, "git", "config", "--global", key, value).CombinedOutput(); err != nil {
			return fmt.Errorf("set git %s: %v: %s", key, err, lastLine(out))
		}
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
	st.ClaudeRestarts, st.ClaudeLastExit = a.restarts, a.lastExit
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
