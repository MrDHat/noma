//go:build e2e

package e2e

import (
	"context"
	"regexp"
	"testing"
)

var linkPattern = regexp.MustCompile(`https://claude\.ai/code/session_[A-Za-z0-9]+`)

func TestUpPrintsRemoteControlLink(t *testing.T) {
	out := mustNoma(t, "up", "--name", "e2e-link", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-link") })
	link := linkPattern.FindString(out)
	if link == "" {
		t.Fatalf("noma up printed no Remote Control link:\n%s", out)
	}
	ws, err := client(t).Workspace(context.Background(), "e2e-link")
	if err != nil {
		t.Fatal(err)
	}
	if ws.Link != link {
		t.Errorf("stored link %q, printed %q", ws.Link, link)
	}
}
