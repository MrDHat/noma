package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MrDHat/noma/internal/api"
)

// setupFile on the workspace volume records how far the repo's setup list
// got (M3.5-12): the hash of the list it ran and how many of its commands
// succeeded. A later start skips a list that ran in full, and resumes a
// failed one from the command that failed.
const setupFile = ".noma-setup.json"

// setupTimeout bounds each setup command.
const setupTimeout = 30 * time.Minute

type setupState struct {
	Hash string `json:"hash"`
	Done int    `json:"done"`
}

func setupHash(cmds []string) string {
	b, _ := json.Marshal(cmds)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// runSetup runs the setup commands noma.yaml listed when the agent started,
// in the checkout, with the workspace's credentials. It records each
// command that succeeds, so the record always describes the list it read,
// never a newer one. A failed command's error names it with its last
// lines; the caller reports it and does not start Claude.
func (a *Agent) runSetup(ctx context.Context, repo string, cmds []string) error {
	if len(cmds) == 0 {
		return nil
	}
	path := filepath.Join(a.Dir, setupFile)
	st := setupState{Hash: setupHash(cmds)}
	var saved setupState
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &saved) == nil && saved.Hash == st.Hash {
		st.Done = min(saved.Done, len(cmds))
	}
	for i := st.Done; i < len(cmds); i++ {
		if err := a.runSetupCommand(ctx, repo, i, cmds); err != nil {
			return err
		}
		st.Done = i + 1
		if err := saveSetup(path, st); err != nil {
			return fmt.Errorf("record setup progress: %w", err)
		}
	}
	return nil
}

func (a *Agent) runSetupCommand(ctx context.Context, repo string, i int, cmds []string) error {
	step := fmt.Sprintf("%d/%d: %s", i+1, len(cmds), cmds[i])
	a.report(ctx, api.AgentStatus{Phase: api.PhaseSetup, Message: step})
	cctx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "sh", "-c", cmds[i])
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), a.vault.env()...)
	cmd.WaitDelay = 5 * time.Second
	out := &tailBuffer{max: 4 << 10}
	cmd.Stdout, cmd.Stderr = tailWriter{out}, tailWriter{out}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s %s: %w", api.SetupFailed, step, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	// Show the command's latest output line while it runs.
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	shown := ""
	for {
		select {
		case err := <-done:
			if err == nil {
				return nil
			}
			if errors.Is(cctx.Err(), context.DeadlineExceeded) {
				err = fmt.Errorf("timed out after %v", setupTimeout)
			}
			return fmt.Errorf("%s %s: %v\n%s", api.SetupFailed, step, err, lastLines(out.text(), 10))
		case <-tick.C:
			if line := out.lastLine(); line != "" && line != shown {
				shown = line
				a.report(ctx, api.AgentStatus{Phase: api.PhaseSetup, Message: step + " | " + line})
			}
		}
	}
}

func saveSetup(path string, st setupState) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// lastLines returns the last n non-empty lines of s.
func lastLines(s string, n int) string {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
