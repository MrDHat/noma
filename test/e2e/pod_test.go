//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MrDHat/noma/internal/api"
)

// Demo step 5: a deleted pod comes back on the same volume with the same
// link, and the workspace never claims to be ready while it is gone.
func TestDeletedPodComesBack(t *testing.T) {
	ctx := context.Background()
	c := client(t)
	out := mustNoma(t, "up", "--name", "e2e-pod", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-pod") })
	link := linkPattern.FindString(out)
	ws, err := c.Workspace(ctx, "e2e-pod")
	if err != nil {
		t.Fatal(err)
	}
	pod, marker := ws.ID+"-0", "/workspaces/"+ws.ID+"/e2e-pod/uncommitted-work"
	kubectl(t, "-n", userNamespace, "exec", pod, "--", "touch", marker)
	oldUID := kubectl(t, "-n", userNamespace, "get", "pod", pod, "-o", "jsonpath={.metadata.uid}")

	kubectl(t, "-n", userNamespace, "delete", "pod", pod, "--wait=true")
	// The old pod is gone: the workspace must stop saying it is ready
	// within moments, then be ready again once the new pod's agent reports.
	var phases []string
	left := false
	start := time.Now()
	for {
		ws, err = c.Workspace(ctx, "e2e-pod")
		if err != nil {
			t.Fatal(err)
		}
		if len(phases) == 0 || phases[len(phases)-1] != ws.Phase {
			phases = append(phases, ws.Phase)
		}
		left = left || ws.Phase != api.PhaseReady
		if left && ws.Phase == api.PhaseReady {
			break
		}
		switch {
		case !left && time.Since(start) > 10*time.Second:
			t.Fatalf("workspace still says ready 10s after its pod was deleted; phases %v", phases)
		case ws.Phase == api.PhaseError || time.Since(start) > 3*time.Minute:
			t.Fatalf("workspace did not come back; phases %v, message %q", phases, ws.Message)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("phases after the pod was deleted: %v", phases)
	if ws.Link != link {
		t.Fatalf("link after the pod came back = %q, want the same %q", ws.Link, link)
	}
	if uid := kubectl(t, "-n", userNamespace, "get", "pod", pod, "-o", "jsonpath={.metadata.uid}"); uid == oldUID {
		t.Fatal("the pod was not replaced")
	}
	kubectl(t, "-n", userNamespace, "exec", pod, "--", "test", "-f", marker)
	if out := mustNoma(t, "ls"); !strings.Contains(out, link) {
		t.Fatalf("noma ls lacks the link:\n%s", out)
	}
}
