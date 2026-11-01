package agent

import (
	"context"
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
}

// Run sets the workspace up, then serves server messages until ctx is done.
// A failed setup is reported as an error phase; the agent keeps running so
// the pod doesn't crash-loop.
func (a *Agent) Run(ctx context.Context) error {
	cfg, err := retry(ctx, func() (*api.AgentConfig, error) { return a.Client.Config(ctx) })
	if err != nil {
		return err
	}
	slog.Info("workspace", "id", cfg.ID, "name", cfg.Name)

	if err := a.setup(ctx, cfg); err != nil {
		slog.Error("setup failed", "err", err)
		a.report(ctx, api.PhaseError, err.Error())
	} else {
		a.report(ctx, api.PhaseReady, "")
	}
	return a.serve(ctx)
}

func (a *Agent) setup(ctx context.Context, cfg *api.AgentConfig) error {
	if err := os.MkdirAll(os.Getenv("HOME"), 0o700); err != nil {
		return fmt.Errorf("create home: %w", err)
	}
	repo := filepath.Join(a.Dir, repoDir(cfg))
	if _, err := os.Stat(filepath.Join(repo, ".git")); err == nil {
		return nil // cloned before this pod restarted
	}
	a.report(ctx, api.PhaseCloning, cfg.RepoURL)
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
		if m != nil {
			slog.Warn("unknown message", "type", m.Type)
		}
	}
	return nil
}

// report sends a status, retrying until the server takes it.
func (a *Agent) report(ctx context.Context, phase, message string) {
	_, _ = retry(ctx, func() (struct{}, error) { return struct{}{}, a.Client.Status(ctx, phase, message) })
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
