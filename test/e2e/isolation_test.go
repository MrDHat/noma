//go:build e2e

package e2e

import (
	"context"
	"os/exec"
	"testing"
)

// A workspace reaches the internet but not the Kubernetes API, the
// kubelet, the metadata service, Postgres or other pods (M3.5-10). make
// demo-aws runs the same check on EKS.
func TestWorkspaceIsIsolated(t *testing.T) {
	mustNoma(t, "up", "--name", "e2e-isolation", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-isolation") })
	ws, err := client(t).Workspace(context.Background(), "e2e-isolation")
	if err != nil {
		t.Fatal(err)
	}
	pod := ws.ID + "-0"
	kubectl(t, "-n", userNamespace, "wait", "pod/"+pod, "--for=condition=Ready", "--timeout=180s")
	node := kubectl(t, "-n", userNamespace, "get", "pod", pod, "-o", "jsonpath={.status.hostIP}")
	server := kubectl(t, "-n", "noma-system", "get", "pod", "-l", "app=noma-server",
		"-o", "jsonpath={.items[0].status.podIP}")
	out, err := exec.Command("kubectl", "--context", kubeContext(), "-n", userNamespace, "exec", pod, "--",
		"noma-agent", "isolation-check", "--kubelet", node, "--pod", server+":8080").CombinedOutput()
	t.Logf("isolation check:\n%s", out)
	if err != nil {
		t.Fatalf("isolation check failed: %v", err)
	}
}
