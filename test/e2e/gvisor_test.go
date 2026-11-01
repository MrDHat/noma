//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

func TestPodRunsUnderGVisor(t *testing.T) {
	kubectl(t, "run", "e2e-gvisor", "--restart=Never", "--image=busybox:1.37",
		`--overrides={"spec":{"runtimeClassName":"gvisor"}}`, "--", "dmesg")
	t.Cleanup(func() { kubectl(t, "delete", "pod", "e2e-gvisor", "--wait=false") })
	kubectl(t, "wait", "pod/e2e-gvisor", "--for=jsonpath={.status.phase}=Succeeded", "--timeout=120s")
	if out := kubectl(t, "logs", "e2e-gvisor"); !strings.Contains(out, "Starting gVisor") {
		t.Fatalf("pod is not running under gVisor; dmesg:\n%s", out)
	}
}
