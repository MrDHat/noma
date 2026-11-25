//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/MrDHat/noma/internal/api"
)

// A report from an agent older than the one that last reported changes
// nothing (NOM-70): a container that crashed with a report in flight
// can't overwrite what its replacement said.
func TestStaleAgentReportIsIgnored(t *testing.T) {
	mustNoma(t, "up", "--name", "e2e-stale", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-stale") })
	ws, err := client(t).Workspace(context.Background(), "e2e-stale")
	if err != nil {
		t.Fatal(err)
	}
	pod := ws.ID + "-0"
	uid := kubectl(t, "-n", userNamespace, "get", "pod", pod, "-o", "jsonpath={.metadata.uid}")
	token := strings.TrimSpace(kubectl(t, "-n", userNamespace, "create", "token", ws.ID,
		"--audience", api.TokenAudience, "--bound-object-kind", "Pod",
		"--bound-object-name", pod, "--bound-object-uid", uid))
	base := agentEndpoint(t)
	report := func(boot int64) {
		t.Helper()
		body, _ := json.Marshal(api.AgentStatus{Phase: api.PhaseClaude, Message: "from the past", Boot: boot})
		req, _ := http.NewRequest(http.MethodPost, base+"/agent/v1/status", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := agentHTTP(t).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status report: %d", resp.StatusCode)
		}
	}

	// The workspace's agent is on its first boot, so boot 0 is older.
	report(0)
	if ws, err = client(t).Workspace(context.Background(), "e2e-stale"); err != nil {
		t.Fatal(err)
	}
	if ws.Phase != api.PhaseReady {
		t.Fatalf("a stale report moved the phase to %q (%q)", ws.Phase, ws.Message)
	}

	// The same report from a newer boot lands, so the token and route work.
	report(1 << 20)
	if ws, err = client(t).Workspace(context.Background(), "e2e-stale"); err != nil {
		t.Fatal(err)
	}
	if ws.Phase != api.PhaseClaude {
		t.Fatalf("a newer report left the phase at %q", ws.Phase)
	}
}
