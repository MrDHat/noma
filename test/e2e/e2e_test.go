//go:build e2e

// Package e2e runs the M2 demo against a cluster brought up by make dev-up.
package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MrDHat/noma/internal/cli"
)

// nomaBin is the noma CLI built for this test run.
var nomaBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "noma-e2e")
	if err != nil {
		panic(err)
	}
	nomaBin = filepath.Join(dir, "noma")
	if out, err := exec.Command("go", "build", "-o", nomaBin, "../../cmd/noma").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build noma: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

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

// noma runs the CLI and returns its combined output and exit error.
func noma(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, err := exec.Command(nomaBin, args...).CombinedOutput()
	return string(out), err
}

// mustNoma runs the CLI and fails the test if it exits non-zero.
func mustNoma(t *testing.T, args ...string) string {
	t.Helper()
	out, err := noma(t, args...)
	if err != nil {
		t.Fatalf("noma %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// client returns an API client using the CLI's config.
func client(t *testing.T) *cli.Client {
	t.Helper()
	cfg, err := cli.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	c, err := cli.Connect(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}
