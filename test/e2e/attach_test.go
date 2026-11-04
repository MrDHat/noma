//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"

	"github.com/MrDHat/noma/internal/api"
)

// Demo step 2: two workspaces side by side; ls lists them and attach picks
// one up after noma up was cut off during its sign-in.
func TestListAndAttach(t *testing.T) {
	signOut(t)
	// No code to paste: noma up gives up at the sign-in, like Ctrl-C.
	out, err := nomaWithInput(t, "", "up", "--name", "e2e-attach-a", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-attach-a") })
	if err == nil || !strings.Contains(out, "Paste code:") {
		t.Fatalf("noma up should stop at the sign-in prompt; err %v:\n%s", err, out)
	}
	if got := lsRows(t)["e2e-attach-a"]; len(got) != 4 || got[1] != api.PhaseSignIn || got[3] != "-" {
		t.Fatalf("ls row for e2e-attach-a = %q, want phase sign-in and no link", got)
	}

	out = mustNoma(t, "attach", "e2e-attach-a")
	if !strings.Contains(out, "Paste code:") || !linkPattern.MatchString(out) {
		t.Fatalf("attach should finish the sign-in and print the link:\n%s", out)
	}
	mustNoma(t, "up", "--name", "e2e-attach-b", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-attach-b") })

	rows := lsRows(t)
	for _, name := range []string{"e2e-attach-a", "e2e-attach-b"} {
		if got := rows[name]; len(got) != 4 || got[1] != api.PhaseReady || got[2] != demoRepo || !linkPattern.MatchString(got[3]) {
			t.Fatalf("ls row for %s = %q, want ready with the repo and a link", name, got)
		}
	}
	// Attaching to a ready workspace just prints its link.
	if out = mustNoma(t, "attach", "e2e-attach-b"); !strings.Contains(out, rows["e2e-attach-b"][3]) {
		t.Fatalf("attach to a ready workspace printed a different link:\n%s", out)
	}

	mustNoma(t, "stop", "e2e-attach-b")
	out, err = noma(t, "attach", "e2e-attach-b")
	if err == nil || !strings.Contains(out, "noma start e2e-attach-b") {
		t.Fatalf("attach to a stopped workspace should refuse; err %v:\n%s", err, out)
	}
	if _, err := noma(t, "attach", "e2e-no-such"); err == nil {
		t.Fatal("attach to a missing workspace succeeded")
	}
}

func TestCreateRejectsLargeBody(t *testing.T) {
	_, err := client(t).CreateWorkspace(context.Background(), api.CreateWorkspaceRequest{
		RepoURL: "https://example.com/" + strings.Repeat("a", 32<<10),
	})
	if err == nil || !strings.Contains(err.Error(), "(400)") {
		t.Fatalf("a 32 KiB create request got %v, want a 400", err)
	}
}

// lsRows runs noma ls and returns its rows by workspace name.
func lsRows(t *testing.T) map[string][]string {
	t.Helper()
	rows := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(mustNoma(t, "ls")), "\n")[1:] {
		f := strings.Fields(line)
		rows[f[0]] = f
	}
	return rows
}
