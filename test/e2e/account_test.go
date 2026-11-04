//go:build e2e

package e2e

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// A token for another Claude account planted in the login stops Claude
// from starting again: its work would go to that account (M3-26).
func TestPlantedAccountStopsClaude(t *testing.T) {
	mustNoma(t, "up", "--name", "e2e-account", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-account") })
	ws, err := client(t).Workspace(context.Background(), "e2e-account")
	if err != nil {
		t.Fatal(err)
	}
	pod := ws.ID + "-0"
	creds := `"${CLAUDE_SECURESTORAGE_CONFIG_DIR:-$HOME/.claude}/.credentials.json"`
	// Put the real login back afterwards; later tests share it.
	saved := kubectl(t, "-n", userNamespace, "exec", pod, "--", "sh", "-c", "cat "+creds)
	t.Cleanup(func() {
		cmd := exec.Command("kubectl", "--context", kubeContext(), "-n", userNamespace, "exec", "-i", pod, "--", "sh", "-c", "cat > "+creds)
		cmd.Stdin = strings.NewReader(saved)
		_ = cmd.Run()
	})

	intruder := `{"claudeAiOauth":{"accessToken":"fake-token-intruder","expiresAt":` + "9999999999999" + `}}`
	kubectl(t, "-n", userNamespace, "exec", pod, "--", "sh", "-c",
		"printf '%s' '"+intruder+"' > "+creds+
			` && for p in /proc/[0-9]*; do grep -q "remote-[c]ontrol" "$p/cmdline" 2>/dev/null && kill "${p#/proc/}"; done; true`)

	deadline := time.Now().Add(time.Minute)
	for {
		ws, err = client(t).Workspace(context.Background(), "e2e-account")
		if err == nil && ws.Phase == "error" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the workspace kept going on another account's login: %+v", ws)
		}
		time.Sleep(time.Second)
	}
	if ws.Message != "Claude account changed" {
		t.Fatalf("error message %q, want %q", ws.Message, "Claude account changed")
	}
	out := kubectl(t, "-n", userNamespace, "exec", pod, "--", "sh", "-c",
		`for p in /proc/[0-9]*; do grep -q "remote-[c]ontrol" "$p/cmdline" 2>/dev/null && echo running; done; true`)
	if strings.Contains(out, "running") {
		t.Fatal("Claude is running on the planted login")
	}
}
