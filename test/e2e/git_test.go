//go:build e2e

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The git identity on the laptop is the identity of commits made in the
// workspace.
func TestCommitsCarryTheLaptopGitIdentity(t *testing.T) {
	gitconfig := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(gitconfig, []byte("[user]\n\tname = Noma E2E\n\temail = e2e@noma.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(nomaBin, "up", "--name", "e2e-git", demoRepo)
	cmd.Stdin = strings.NewReader(signInCode + "\n")
	cmd.Env = append(os.Environ(), "NOMA_NO_BROWSER=1", "GIT_CONFIG_GLOBAL="+gitconfig, "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-git") })
	if err != nil {
		t.Fatalf("noma up: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "warning: git") {
		t.Fatalf("noma up warned about a git identity that is set:\n%s", out)
	}
	ws, err := client(t).Workspace(context.Background(), "e2e-git")
	if err != nil {
		t.Fatal(err)
	}
	repo := "/workspaces/" + ws.ID + "/e2e-git"
	kubectl(t, "-n", userNamespace, "exec", ws.ID+"-0", "--", "git", "-C", repo, "commit", "--allow-empty", "-qm", "e2e")
	author := kubectl(t, "-n", userNamespace, "exec", ws.ID+"-0", "--", "git", "-C", repo, "log", "-1", "--format=%an <%ae>")
	if strings.TrimSpace(author) != "Noma E2E <e2e@noma.test>" {
		t.Fatalf("commit author = %q, want the laptop's identity", author)
	}
}
