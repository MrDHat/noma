//go:build e2e

package e2e

import (
	"os/exec"
	"strings"
	"testing"
)

// The chart's preflight check fails on a cluster without the RuntimeClass
// Noma needs, and says what is missing (M3.5-15). Helm runs the same Job as
// a pre-install hook; make dev-up's install covers the passing case.
func TestPreflightNamesAMissingRuntimeClass(t *testing.T) {
	const ns = "e2e-preflight"
	kubectl(t, "create", "namespace", ns)
	t.Cleanup(func() {
		_, _ = exec.Command("kubectl", "--context", kubeContext(), "delete", "namespace", ns, "--wait=false").CombinedOutput()
		_, _ = exec.Command("kubectl", "--context", kubeContext(), "delete",
			"clusterrole,clusterrolebinding", "e2e-preflight-"+ns, "--ignore-not-found").CombinedOutput()
	})
	out, err := exec.Command("helm", "template", "e2e", "../../charts/noma", "-n", ns,
		"-f", "../../charts/noma/values-minikube.yaml",
		"--set", "workspace.runtimeClassName=e2e-missing",
		"--show-only", "templates/preflight.yaml").Output()
	if err != nil {
		t.Fatalf("helm template: %v", err)
	}
	apply := exec.Command("kubectl", "--context", kubeContext(), "-n", ns, "apply", "-f", "-")
	apply.Stdin = strings.NewReader(string(out))
	if b, err := apply.CombinedOutput(); err != nil {
		t.Fatalf("apply the preflight Job: %v\n%s", err, b)
	}
	kubectl(t, "-n", ns, "wait", "job/e2e-preflight", "--for=condition=Failed", "--timeout=120s")
	logs := kubectl(t, "-n", ns, "logs", "job/e2e-preflight")
	if !strings.Contains(logs, `RuntimeClass "e2e-missing" does not exist`) {
		t.Fatalf("the preflight check did not name the missing RuntimeClass:\n%s", logs)
	}
}
