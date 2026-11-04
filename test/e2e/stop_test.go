//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"

	"github.com/MrDHat/noma/internal/api"
)

// Demo step 3: stop then start brings back the files and the same link.
func TestStopAndStartKeepFilesAndLink(t *testing.T) {
	ctx := context.Background()
	c := client(t)
	out := mustNoma(t, "up", "--name", "e2e-stop", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-stop") })
	link := linkPattern.FindString(out)
	if link == "" {
		t.Fatalf("noma up printed no link:\n%s", out)
	}
	ws, err := c.Workspace(ctx, "e2e-stop")
	if err != nil {
		t.Fatal(err)
	}
	pod, marker := ws.ID+"-0", "/workspaces/"+ws.ID+"/e2e-stop/uncommitted-work"
	kubectl(t, "-n", userNamespace, "exec", pod, "--", "touch", marker)

	out = mustNoma(t, "stop", "e2e-stop")
	if !strings.Contains(out, "stopped") {
		t.Fatalf("noma stop did not report stopped:\n%s", out)
	}
	if ws, err = c.Workspace(ctx, "e2e-stop"); err != nil || ws.Phase != api.PhaseStopped {
		t.Fatalf("after stop: phase %q, err %v; want stopped", ws.Phase, err)
	}
	if got := kubectl(t, "-n", userNamespace, "get", "pods", "-l", "noma.dev/workspace="+ws.ID, "-o", "name"); strings.TrimSpace(got) != "" {
		t.Fatalf("a stopped workspace still has a pod: %s", got)
	}
	kubectl(t, "-n", userNamespace, "get", "pvc", ws.ID) // the volume stays
	mustNoma(t, "stop", "e2e-stop")                      // stopping again does nothing

	out = mustNoma(t, "start", "e2e-stop")
	if got := linkPattern.FindString(out); got != link {
		t.Fatalf("link after start = %q, want the same %q:\n%s", got, link, out)
	}
	kubectl(t, "-n", userNamespace, "exec", pod, "--", "test", "-f", marker)
	mustNoma(t, "start", "e2e-stop") // starting a running workspace does nothing
}
