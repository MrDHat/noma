package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

// Claude runs Claude Code's Remote Control server in the workspace.
type Claude struct {
	// Bin is the claude binary.
	Bin string
	// Home is the user's home, holding ~/.claude.json and ~/.claude.
	Home string
	// Dir is the repo checkout Claude works in.
	Dir string
	// Name is shown in claude.ai/code.
	Name string
}

var (
	linkPattern = regexp.MustCompile(`https://claude\.ai/code\?environment=(env_[A-Za-z0-9]+)`)
	envPattern  = regexp.MustCompile(`^env_[A-Za-z0-9]+$`)
	ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07`)
)

// Link turns a Remote Control environment ID into the claude.ai/code link.
func Link(envID string) string { return "https://claude.ai/code?environment=" + envID }

// Seed pre-answers the prompts Claude shows on first start, so it can run
// unattended: it trusts the checkout and has seen the Remote Control notice.
func (c *Claude) Seed() error {
	path := filepath.Join(c.Home, ".claude.json")
	cfg := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	cfg["hasCompletedOnboarding"] = true
	cfg["remoteDialogSeen"] = true
	projects, _ := cfg["projects"].(map[string]any)
	if projects == nil {
		projects = map[string]any{}
	}
	project, _ := projects[c.Dir].(map[string]any)
	if project == nil {
		project = map[string]any{}
	}
	project["hasTrustDialogAccepted"] = true
	projects[c.Dir] = project
	cfg["projects"] = projects
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// LoggedIn reports whether Claude has a login.
func (c *Claude) LoggedIn(ctx context.Context) (bool, error) {
	// auth status exits 1 when logged out, so judge by the JSON.
	out, _ := c.command(ctx, "auth", "status").Output()
	var st struct {
		LoggedIn *bool `json:"loggedIn"`
	}
	if err := json.Unmarshal(out, &st); err != nil || st.LoggedIn == nil {
		return false, fmt.Errorf("unexpected claude auth status output: %q", lastLine(out))
	}
	return *st.LoggedIn, nil
}

// RemoteControl runs `claude remote-control` in a PTY until it exits or ctx
// is done. It calls onLink once the environment's link is known. Remote
// Control prompts on a terminal, so it can't run without one.
func (c *Claude) RemoteControl(ctx context.Context, onLink func(string)) error {
	cmd := c.command(ctx, "remote-control", "--name", c.Name, "--spawn", "same-dir")
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		return fmt.Errorf("start claude: %w", err)
	}
	defer tty.Close() //nolint:errcheck

	var once sync.Once
	found := func(envID string) { once.Do(func() { onLink(Link(envID)) }) }
	tail := &tailBuffer{max: 4 << 10}
	go func() {
		// The link is printed, and also recorded in bridge-pointer.json.
		_, _ = io.Copy(linkWatcher{tail: tail, found: found}, tty)
	}()
	stop := make(chan struct{})
	defer close(stop)
	go c.watchBridgePointer(stop, found)

	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("claude remote-control exited: %v: %s", err, tail.lastLine())
	}
	return nil
}

// watchBridgePointer looks for the environment ID Claude records for the
// checkout under ~/.claude/projects/<path>/bridge-pointer.json.
func (c *Claude) watchBridgePointer(stop <-chan struct{}, found func(string)) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		files, _ := filepath.Glob(filepath.Join(c.Home, ".claude", "projects", "*", "bridge-pointer.json"))
		for _, f := range files {
			if id := envIDIn(f); id != "" {
				found(id)
				return
			}
		}
	}
}

// envIDIn returns the first env_… value in a JSON file. The file's layout is
// Claude's, so don't depend on field names.
func envIDIn(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var v map[string]any
	if json.Unmarshal(b, &v) != nil {
		return ""
	}
	for _, x := range v {
		if s, ok := x.(string); ok && envPattern.MatchString(s) {
			return s
		}
	}
	return ""
}

func (c *Claude) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), "HOME="+c.Home, "TERM=xterm-256color", "DISABLE_AUTOUPDATER=1")
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

// linkWatcher scans terminal output for the Remote Control link.
type linkWatcher struct {
	tail  *tailBuffer
	found func(string)
}

func (w linkWatcher) Write(p []byte) (int, error) {
	w.tail.Write(p)
	if m := linkPattern.FindStringSubmatch(w.tail.text()); m != nil {
		w.found(m[1])
	}
	return len(p), nil
}

// tailBuffer keeps the last max bytes of output, for error messages.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tailBuffer) Write(p []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = t.b[len(t.b)-t.max:]
	}
}

// raw returns the output as written, with escape sequences. Claude wraps
// the sign-in URL in a terminal hyperlink, so the plain URL is in there too.
func (t *tailBuffer) raw() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}

func (t *tailBuffer) text() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return ansiPattern.ReplaceAllString(string(t.b), "")
}

func (t *tailBuffer) lastLine() string {
	return lastLine([]byte(strings.ReplaceAll(t.text(), "\r", "")))
}
