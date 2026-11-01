//go:build e2e

// Package e2e runs the M2 demo against a cluster brought up by make dev-up.
package e2e

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func kubeContext() string {
	if p := os.Getenv("NOMA_PROFILE"); p != "" {
		return p
	}
	return "noma"
}

// kubectl runs kubectl against the dev cluster and returns its combined output.
func kubectl(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("kubectl", append([]string{"--context", kubeContext()}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
