//go:build e2e

package e2e

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os/exec"
	"strings"
	"testing"
)

// A git token reaches the workspaces its rule matches, through the
// agent's credential helper, and no others.
func TestGitCredentialReachesMatchingWorkspaces(t *testing.T) {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	token := "ghp_e2e" + hex.EncodeToString(b)

	for _, args := range [][]string{
		{"e2e-git-match", "--git-host", "git.example.com", "--repos", "github.com/octocat/*"},
		{"e2e-git-other", "--git-host", "other.example.com", "--repos", "github.com/acme/*"},
	} {
		if out, err := nomaWithInput(t, token+"\n", append([]string{"secret", "set"}, args...)...); err != nil {
			t.Fatalf("noma secret set: %v\n%s", err, out)
		}
		name := args[0]
		t.Cleanup(func() { _, _ = noma(t, "secret", "rm", name) })
	}
	mustNoma(t, "up", "--name", "e2e-git", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-git") })
	ws, err := client(t).Workspace(context.Background(), "e2e-git")
	if err != nil {
		t.Fatal(err)
	}

	fill := func(host string) (string, error) {
		cmd := exec.Command("kubectl", "--context", kubeContext(), "-n", userNamespace, "exec", "-i", ws.ID+"-0", "--",
			"env", "GIT_TERMINAL_PROMPT=0", "git", "credential", "fill")
		cmd.Stdin = strings.NewReader("protocol=https\nhost=" + host + "\n\n")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := fill("git.example.com")
	if err != nil || !strings.Contains(out, "password="+token) {
		t.Fatalf("git credential fill for git.example.com: %v\n%s", err, out)
	}
	if out, err := fill("other.example.com"); err == nil || strings.Contains(out, token) {
		t.Fatalf("a secret for github.com/acme/* reached a workspace on octocat: %v\n%s", err, out)
	}
	// git ran the helper and asked to store the token; nothing may have.
	if found := kubectl(t, "-n", userNamespace, "exec", ws.ID+"-0", "--", "sh", "-c",
		`grep -rl "$0" "$NOMA_WORKSPACE_DIR" || true`, token); strings.TrimSpace(found) != "" {
		t.Fatalf("the token was written to the workspace volume:\n%s", found)
	}
}
