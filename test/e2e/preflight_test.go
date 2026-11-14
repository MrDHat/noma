//go:build e2e

package e2e

import (
	"os/exec"
	"strings"
	"testing"
)

// The chart refuses to install on a cluster without the RuntimeClass it
// needs, and says what is missing (M3.5-15).
func TestChartRefusesAClusterWithoutItsRuntimeClass(t *testing.T) {
	const ns = "e2e-preflight"
	helm := func(args ...string) (string, error) {
		out, err := exec.Command("helm", append([]string{"--kube-context", kubeContext()}, args...)...).CombinedOutput()
		return string(out), err
	}
	t.Cleanup(func() {
		_, _ = helm("uninstall", "preflight", "-n", ns, "--no-hooks")
		_, _ = exec.Command("kubectl", "--context", kubeContext(), "delete", "namespace", ns, "--wait=false").CombinedOutput()
	})
	out, err := helm("install", "preflight", "../../charts/noma", "-n", ns, "--create-namespace",
		"-f", "../../charts/noma/values-minikube.yaml",
		"--set", "workspace.runtimeClassName=e2e-missing", "--wait", "--timeout", "3m")
	if err == nil {
		t.Fatalf("the chart installed without its RuntimeClass:\n%s", out)
	}
	logs := kubectl(t, "-n", ns, "logs", "job/preflight-preflight")
	if !strings.Contains(logs, `RuntimeClass "e2e-missing" does not exist`) {
		t.Fatalf("the preflight check did not name the missing RuntimeClass:\n%s", logs)
	}
	// Nothing past the check was installed.
	if left := kubectl(t, "-n", ns, "get", "deployments,statefulsets", "-o", "jsonpath={.items[*].metadata.name}"); strings.TrimSpace(left) != "" {
		t.Fatalf("the failed install left objects behind:\n%s", left)
	}
}
