//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

func TestPodRunsUnderGVisor(t *testing.T) {
	out := kubectl(t, "run", "e2e-gvisor", "--rm", "-i", "--restart=Never",
		"--image=busybox:1.37", `--overrides={"spec":{"runtimeClassName":"gvisor"}}`,
		"--", "dmesg")
	if !strings.Contains(out, "Starting gVisor") {
		t.Fatalf("pod is not running under gVisor; dmesg:\n%s", out)
	}
}
