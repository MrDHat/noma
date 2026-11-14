//go:build e2e

package e2e

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/MrDHat/noma/internal/api"
)

// runUninstallGuard runs the chart's pre-delete hook Job under its own
// release name, without uninstalling, and returns whether it passed and
// its log.
func runUninstallGuard(t *testing.T) (bool, string) {
	t.Helper()
	out, err := exec.Command("helm", "template", "e2e", "../../charts/noma", "-n", "noma-system",
		"-f", "../../charts/noma/values-minikube.yaml",
		"--show-only", "templates/uninstall-guard.yaml").Output()
	if err != nil {
		t.Fatalf("helm template: %v", err)
	}
	_, _ = exec.Command("kubectl", "--context", kubeContext(), "-n", "noma-system", "delete", "job", "e2e-uninstall-guard", "--ignore-not-found", "--wait").CombinedOutput()
	apply := exec.Command("kubectl", "--context", kubeContext(), "-n", "noma-system", "apply", "-f", "-")
	apply.Stdin = strings.NewReader(string(out))
	if b, err := apply.CombinedOutput(); err != nil {
		t.Fatalf("apply the uninstall guard Job: %v\n%s", err, b)
	}
	t.Cleanup(func() {
		_, _ = exec.Command("kubectl", "--context", kubeContext(), "-n", "noma-system", "delete", "job", "e2e-uninstall-guard", "--wait=false").CombinedOutput()
	})
	_, err = exec.Command("kubectl", "--context", kubeContext(), "-n", "noma-system", "wait", "job/e2e-uninstall-guard",
		"--for=jsonpath={.status.conditions[?(@.status==\"True\")].type}=Complete", "--timeout=90s").CombinedOutput()
	logs := kubectl(t, "-n", "noma-system", "logs", "job/e2e-uninstall-guard")
	return err == nil, logs
}

// helm uninstall fails while a workspace exists (M3.5-18).
func TestUninstallRefusedWhileWorkspacesExist(t *testing.T) {
	mustNoma(t, "up", "--name", "e2e-guard", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-guard") })
	passed, logs := runUninstallGuard(t)
	if passed || !strings.Contains(logs, "workspaces still exist") {
		t.Fatalf("the uninstall guard passed with a workspace running:\n%s", logs)
	}
	// Refusing changed nothing: workspaces can still be created.
	if _, err := client(t).Workspace(context.Background(), "e2e-guard"); err != nil {
		t.Fatal(err)
	}
}

// With no workspaces left the guard passes, creates are refused from then
// on, and helm uninstall goes through (M3.5-19). It ends the install, so CI
// runs it alone after the rest of the suite.
func TestUninstallClosesTheInstall(t *testing.T) {
	if os.Getenv("NOMA_E2E_UNINSTALL") != "1" {
		t.Skip("ends the install; set NOMA_E2E_UNINSTALL=1 to run it")
	}
	for _, ws := range lsRows(t) {
		_, _ = noma(t, "rm", ws[0])
	}
	waitNoWorkspaces(t)
	if passed, logs := runUninstallGuard(t); !passed {
		t.Fatalf("the uninstall guard refused with no workspaces:\n%s", logs)
	}
	_, err := client(t).CreateWorkspace(context.Background(), api.CreateWorkspaceRequest{RepoURL: demoRepo, Name: "e2e-late"})
	if err == nil || !strings.Contains(err.Error(), "being uninstalled") {
		t.Fatalf("create after close = %v, want it refused", err)
	}
	if out, err := exec.Command("helm", "uninstall", "noma", "-n", "noma-system", "--kube-context", kubeContext(), "--wait").CombinedOutput(); err != nil {
		t.Fatalf("helm uninstall: %v\n%s", err, out)
	}
}

// waitNoWorkspaces waits until the server has no workspace rows left,
// removed ones included.
func waitNoWorkspaces(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for {
		list, err := client(t).Workspaces(context.Background())
		if err == nil && len(list) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("workspaces left after 3 minutes: %v %v", list, err)
		}
		time.Sleep(2 * time.Second)
	}
}
