package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MrDHat/noma/internal/api"
)

func setupAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	// No server: reports go nowhere.
	return &Agent{Dir: dir, sendStatus: func(context.Context, api.AgentStatus) error { return nil }}, repo
}

func TestSetupRunsOnceAndResumesAfterAFailure(t *testing.T) {
	a, repo := setupAgent(t)
	log := filepath.Join(repo, "log")
	cmds := []string{"echo one >> log", "test -f ok || { echo broken; exit 3; }", "echo three >> log"}

	err := a.runSetup(context.Background(), repo, cmds)
	if err == nil || !strings.Contains(err.Error(), "setup failed: 2/3") || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("want the second command's failure with its output, got %v", err)
	}
	// Fixed, and started again: it resumes at the command that failed.
	if err := os.WriteFile(filepath.Join(repo, "ok"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.runSetup(context.Background(), repo, cmds); err != nil {
		t.Fatal(err)
	}
	// A third start skips the list that ran in full.
	if err := a.runSetup(context.Background(), repo, cmds); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, log); got != "one\nthree\n" {
		t.Errorf("commands ran %q, want each once", got)
	}
	// A changed list runs from the start.
	if err := a.runSetup(context.Background(), repo, append(cmds, "true")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, log); got != "one\nthree\none\nthree\n" {
		t.Errorf("a changed list ran %q", got)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
