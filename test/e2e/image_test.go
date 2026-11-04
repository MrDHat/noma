//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/MrDHat/noma/internal/api"
)

// A repo's noma.yaml picks the workspace image: the workspace restarts on
// it once, with Noma's binaries copied in, and keeps its files and link.
func TestNomaYAMLImage(t *testing.T) {
	ctx := context.Background()
	c := client(t)
	out := mustNoma(t, "up", "--name", "e2e-image", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-image") })
	link := linkPattern.FindString(out)
	ws, err := c.Workspace(ctx, "e2e-image")
	if err != nil {
		t.Fatal(err)
	}
	pod, repo := ws.ID+"-0", "/workspaces/"+ws.ID+"/e2e-image"

	// noma.yaml is read at every start, so adding one and restarting is
	// the same as cloning a repo that has it.
	kubectl(t, "-n", userNamespace, "exec", pod, "--", "bash", "-c",
		"echo 'image: noma-e2e-project:dev' > "+repo+"/noma.yaml")
	mustNoma(t, "stop", "e2e-image")
	out = mustNoma(t, "start", "e2e-image")
	if got := linkPattern.FindString(out); got != link {
		t.Fatalf("link on the project image = %q, want the same %q:\n%s", got, link, out)
	}
	image := kubectl(t, "-n", userNamespace, "get", "pod", pod, "-o", "jsonpath={.spec.containers[0].image}")
	if image != "noma-e2e-project:dev" {
		t.Fatalf("workspace runs %q, want the project image", image)
	}
	kubectl(t, "-n", userNamespace, "exec", pod, "--", "test", "-f", "/etc/noma-e2e-project")
	kubectl(t, "-n", userNamespace, "exec", pod, "--", "test", "-f", repo+"/README")
	if rc := kubectl(t, "-n", userNamespace, "get", "pod", pod, "-o", "jsonpath={.spec.runtimeClassName}"); rc != "gvisor" {
		t.Errorf("project-image pod runs under %q, want gvisor", rc)
	}

	// An image without git is refused with an error, not a crash loop.
	kubectl(t, "-n", userNamespace, "exec", pod, "--", "bash", "-c",
		"echo 'image: noma-e2e-nogit:dev' > "+repo+"/noma.yaml")
	mustNoma(t, "stop", "e2e-image")
	if out, err := noma(t, "start", "e2e-image"); err == nil {
		t.Fatalf("a workspace on an image without git started:\n%s", out)
	}
	ws = waitUntil(t, "e2e-image", time.Minute, func(ws *api.Workspace) bool { return ws.Phase == api.PhaseError })
	if !strings.Contains(ws.Message, "needs glibc and git") {
		t.Fatalf("error = %q, want it to say what the image lacks", ws.Message)
	}
}

// waitUntil polls the workspace until done says so, or fails after timeout.
func waitUntil(t *testing.T, name string, timeout time.Duration, done func(*api.Workspace) bool) *api.Workspace {
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
			t.Fatalf("workspace %s: gave up waiting; phase %q, message %q", name, ws.Phase, ws.Message)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
