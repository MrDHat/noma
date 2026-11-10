//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MrDHat/noma/internal/api"
)

// killClaude kills Claude's Remote Control process in the workspace pod.
func killClaude(t *testing.T, pod string) {
	t.Helper()
	kubectl(t, "-n", userNamespace, "exec", pod, "--", "bash", "-c",
		`for p in /proc/[0-9]*; do
		   [ "$(tr '\0' ' ' <"$p/cmdline" 2>/dev/null | cut -c1-21)" = "claude remote-control" ] && kill -KILL "${p#/proc/}"
		 done; true`)
}

// waitFor polls the workspace until done says so, or fails after timeout.
func waitFor(t *testing.T, name string, timeout time.Duration, done func(*api.Workspace) bool) *api.Workspace {
	t.Helper()
	c := client(t)
	deadline := time.Now().Add(timeout)
	for {
		ws, err := c.Workspace(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		if done(ws) {
			return ws
		}
		if time.Now().After(deadline) {
			t.Fatalf("workspace %s: gave up waiting; phase %q, message %q, restarts %d",
				name, ws.Phase, ws.Message, ws.ClaudeRestarts)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Demo step 4: Claude killed inside the workspace comes back by itself with
// the same link, and ls counts the restart.
func TestKilledClaudeRestarts(t *testing.T) {
	out := mustNoma(t, "up", "--name", "e2e-restart", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-restart") })
	link := linkPattern.FindString(out)
	ws, err := client(t).Workspace(context.Background(), "e2e-restart")
	if err != nil {
		t.Fatal(err)
	}
	pod := ws.ID + "-0"

	killClaude(t, pod)
	ws = waitFor(t, "e2e-restart", 2*time.Minute, func(ws *api.Workspace) bool {
		return ws.ClaudeRestarts == 1 && ws.Phase == api.PhaseReady
	})
	if ws.Link != link {
		t.Fatalf("link after the restart = %q, want the same %q", ws.Link, link)
	}
	if !strings.Contains(ws.ClaudeLastExit, "killed") {
		t.Errorf("last exit = %q, want it to say Claude was killed", ws.ClaudeLastExit)
	}
	if row := lsRows(t)["e2e-restart"]; row[2] != "1" {
		t.Errorf("ls shows %q restarts, want 1", row[2])
	}

	// A Claude that crashes on every start is given up on after five
	// quick crashes in a row, until the workspace is stopped and started.
	crash := "/workspaces/" + ws.ID + "/home/.claude/fake-crash"
	kubectl(t, "-n", userNamespace, "exec", pod, "--", "touch", crash)
	killClaude(t, pod)
	ws = waitFor(t, "e2e-restart", 2*time.Minute, func(ws *api.Workspace) bool { return ws.Phase == api.PhaseError })
	if !strings.Contains(ws.Message, "crashed 5 times") || !strings.Contains(ws.ClaudeLastExit, "fake crash") {
		t.Fatalf("error = %q, last exit %q; want the crash limit and Claude's own error", ws.Message, ws.ClaudeLastExit)
	}
	kubectl(t, "-n", userNamespace, "exec", pod, "--", "rm", crash)
	mustNoma(t, "stop", "e2e-restart")
	out = mustNoma(t, "start", "e2e-restart")
	if got := linkPattern.FindString(out); got != link {
		t.Fatalf("link after start = %q, want the same %q", got, link)
	}
	if row := lsRows(t)["e2e-restart"]; row[2] != "0" {
		t.Errorf("after start ls shows %q restarts, want 0", row[2])
	}
}
