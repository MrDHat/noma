//go:build e2e

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MrDHat/noma/internal/cli"
)

// noma up copies the laptop's CLAUDE.md, skills and settings.json into the
// workspace's ~/.claude, and nothing else from ~/.claude (M3.5-5).
func TestClaudeSetupIsCopied(t *testing.T) {
	home := t.TempDir()
	for path, body := range map[string]string{
		".claude/CLAUDE.md":            "e2e instructions\n",
		".claude/skills/e2e/SKILL.md":  "e2e skill\n",
		".claude/settings.json":        `{"e2e": true}`,
		".claude/history.jsonl":        "stays on the laptop\n",
		".claude/skills/e2e/.git/HEAD": "skipped\n",
	} {
		p := filepath.Join(home, path)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	config, err := cli.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		userHome, _ := os.UserHomeDir()
		kubeconfig = filepath.Join(userHome, ".kube", "config")
	}
	cmd := exec.Command(nomaBin, "up", "--name", "e2e-claude-setup", demoRepo)
	cmd.Stdin = strings.NewReader(signInCode + "\n")
	cmd.Env = append(os.Environ(), "NOMA_NO_BROWSER=1", "HOME="+home,
		"NOMA_CONFIG="+config, "KUBECONFIG="+kubeconfig)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-claude-setup") })
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("noma up: %v\n%s", err, out)
	}
	ws, err := client(t).Workspace(context.Background(), "e2e-claude-setup")
	if err != nil {
		t.Fatal(err)
	}
	dir := "/workspaces/" + ws.ID + "/home/.claude"
	got := kubectl(t, "-n", userNamespace, "exec", ws.ID+"-0", "--", "sh", "-c",
		"cd "+dir+" && cat CLAUDE.md skills/e2e/SKILL.md settings.json && ls -a skills/e2e && ls")
	for _, want := range []string{"e2e instructions", "e2e skill", `{"e2e": true}`} {
		if !strings.Contains(got, want) {
			t.Errorf("the workspace's ~/.claude lacks %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"history.jsonl", ".git"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the workspace's ~/.claude has %s:\n%s", unwanted, got)
		}
	}
}
