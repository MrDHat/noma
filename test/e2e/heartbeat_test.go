//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
)

// A running agent's polls are recorded, and ls shows no silence note for it.
func TestAgentHeartbeatIsRecorded(t *testing.T) {
	mustNoma(t, "up", "--name", "e2e-heartbeat", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-heartbeat") })
	ws, err := client(t).Workspace(context.Background(), "e2e-heartbeat")
	if err != nil {
		t.Fatal(err)
	}
	seen := kubectl(t, "-n", "noma-system", "exec", "postgres-0", "--", "psql", "-U", "noma", "-d", "noma", "-tAc",
		"SELECT agent_seen_at > now() - interval '2 minutes' FROM workspaces WHERE id = '"+ws.ID+"'")
	if strings.TrimSpace(seen) != "t" {
		t.Fatalf("agent_seen_at is not recent: %q", seen)
	}
	if ws.AgentSilentSeconds != 0 {
		t.Fatalf("a healthy agent is reported silent for %ds", ws.AgentSilentSeconds)
	}
	if out := mustNoma(t, "ls"); strings.Contains(out, "silent") {
		t.Fatalf("ls warns about a healthy agent:\n%s", out)
	}
}
