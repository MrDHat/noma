//go:build e2e

package e2e

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/MrDHat/noma/internal/cli"
)

const demoRepo = "https://github.com/octocat/Hello-World.git"

func TestUpAndRm(t *testing.T) {
	ctx := context.Background()
	c := client(t)

	out := mustNoma(t, "up", demoRepo)
	if !strings.Contains(out, "workspace hello-world") {
		t.Fatalf("noma up did not name the workspace after the repo:\n%s", out)
	}
	ws, err := c.Workspace(ctx, "hello-world")
	if err != nil {
		t.Fatalf("workspace not found after noma up: %v", err)
	}
	if ws.RepoURL != demoRepo {
		t.Errorf("repoUrl = %q, want %q", ws.RepoURL, demoRepo)
	}

	// A second workspace on the same repo gets a suffix.
	out = mustNoma(t, "up", demoRepo)
	if !strings.Contains(out, "workspace hello-world-2") {
		t.Fatalf("second noma up was not suffixed:\n%s", out)
	}

	mustNoma(t, "rm", "hello-world")
	mustNoma(t, "rm", "hello-world-2")
	if _, err := c.Workspace(ctx, "hello-world"); !errors.Is(err, cli.ErrNotFound) {
		t.Fatalf("workspace still there after noma rm: %v", err)
	}
	if _, err := noma(t, "rm", "hello-world"); err == nil {
		t.Fatal("noma rm of a missing workspace succeeded")
	}
}

func TestRequestWithoutTokenIsRejected(t *testing.T) {
	c := client(t)
	resp, err := http.Get(c.BaseURL + "/v1/workspaces/hello-world")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
