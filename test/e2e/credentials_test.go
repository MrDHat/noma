//go:build e2e

package e2e

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os/exec"
	"strings"
	"testing"
	"time"
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

// Changing a secret reaches a running workspace without stop and start:
// git sees the new token at once, and Claude restarts with the new
// environment on the same link.
func TestChangedSecretReachesRunningWorkspace(t *testing.T) {
	set := func(value string) {
		t.Helper()
		if out, err := nomaWithInput(t, value+"\n", "secret", "set", "e2e-live", "--env", "E2E_TOKEN", "--git-host", "live.example.com"); err != nil {
			t.Fatalf("noma secret set: %v\n%s", err, out)
		}
	}
	set("live-one")
	t.Cleanup(func() { _, _ = noma(t, "secret", "rm", "e2e-live") })
	mustNoma(t, "up", "--name", "e2e-live", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-live") })
	before, err := client(t).Workspace(context.Background(), "e2e-live")
	if err != nil {
		t.Fatal(err)
	}
	// The environment of the running Remote Control process.
	claudeEnv := func() string {
		out, _ := exec.Command("kubectl", "--context", kubeContext(), "-n", userNamespace, "exec", before.ID+"-0", "--", "sh", "-c",
			`for p in /proc/[0-9]*; do if grep -q remote-control "$p/cmdline" 2>/dev/null; then tr '\0' '\n' < "$p/environ"; fi; done`).CombinedOutput()
		return string(out)
	}
	if env := claudeEnv(); !strings.Contains(env, "E2E_TOKEN=live-one") {
		t.Fatalf("Claude's environment lacks the secret:\n%s", env)
	}

	set("live-two")
	deadline := time.Now().Add(time.Minute)
	for !strings.Contains(claudeEnv(), "E2E_TOKEN=live-two") {
		if time.Now().After(deadline) {
			t.Fatalf("Claude never got the new value; its environment:\n%s", claudeEnv())
		}
		time.Sleep(2 * time.Second)
	}
	out := kubectl(t, "-n", userNamespace, "exec", before.ID+"-0", "--", "sh", "-c",
		`printf 'protocol=https\nhost=live.example.com\n\n' | git credential fill`)
	if !strings.Contains(out, "password=live-two") {
		t.Fatalf("git did not get the new token:\n%s", out)
	}
	deadline = time.Now().Add(time.Minute)
	for {
		after, err := client(t).Workspace(context.Background(), "e2e-live")
		if err == nil && after.Phase == "ready" {
			if after.Link != before.Link {
				t.Fatalf("the link changed from %s to %s", before.Link, after.Link)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("workspace not ready again after the restart: %+v", after)
		}
		time.Sleep(time.Second)
	}
}
