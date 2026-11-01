//go:build e2e

package e2e

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

const userNamespace = "noma-u-dev"

func TestWorkspaceObjects(t *testing.T) {
	mustNoma(t, "up", "--name", "e2e-objects", demoRepo)
	ws, err := client(t).Workspace(context.Background(), "e2e-objects")
	if err != nil {
		t.Fatal(err)
	}
	pod := ws.ID + "-0"

	kubectl(t, "-n", userNamespace, "wait", "pod/"+pod, "--for=condition=Ready", "--timeout=180s")
	if rc := kubectl(t, "-n", userNamespace, "get", "pod", pod, "-o", "jsonpath={.spec.runtimeClassName}"); rc != "gvisor" {
		t.Errorf("runtimeClassName = %q, want gvisor", rc)
	}
	if phase := kubectl(t, "-n", userNamespace, "get", "pvc", ws.ID, "-o", "jsonpath={.status.phase}"); phase != "Bound" {
		t.Errorf("volume phase = %q, want Bound", phase)
	}
	kubectl(t, "-n", userNamespace, "get", "networkpolicy", "workspaces")
	kubectl(t, "-n", userNamespace, "get", "serviceaccount", ws.ID)

	mustNoma(t, "rm", "e2e-objects")
	for _, obj := range []string{"statefulset/" + ws.ID, "pvc/" + ws.ID, "serviceaccount/" + ws.ID, "pod/" + pod} {
		out, err := exec.Command("kubectl", "--context", kubeContext(), "-n", userNamespace, "get", obj).CombinedOutput()
		if err == nil || !strings.Contains(string(out), "NotFound") {
			t.Errorf("%s still exists after noma rm: %s", obj, out)
		}
	}
}
