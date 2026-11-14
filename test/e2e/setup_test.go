//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
)

// noma.yaml's setup commands run before Claude, once per list (M3.5-12). A
// failed one stops the workspace short of ready and names the command;
// noma start resumes from it; a changed list runs again.
func TestSetupCommands(t *testing.T) {
	mustNoma(t, "up", "--name", "e2e-setup", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-setup") })
	ws, err := client(t).Workspace(context.Background(), "e2e-setup")
	if err != nil {
		t.Fatal(err)
	}
	repo := "/workspaces/" + ws.ID + "/e2e-setup"
	sh := func(script string) string {
		t.Helper()
		pod := ws.ID + "-0"
		return kubectl(t, "-n", userNamespace, "exec", pod, "--", "sh", "-c", "cd "+repo+" && "+script)
	}
	restart := func() (string, error) {
		t.Helper()
		mustNoma(t, "stop", "e2e-setup")
		return noma(t, "start", "e2e-setup")
	}

	// noma.yaml is read at every start, like a clone that has it.
	sh(`printf 'setup:\n  - echo ran >> setup.log\n  - test -f fixed || { echo not fixed yet; exit 3; }\n' > noma.yaml`)
	out, err := restart()
	if err == nil || !strings.Contains(out, "setup failed: 2/2") || !strings.Contains(out, "not fixed yet") {
		t.Fatalf("want the second command's failure and its output, got %v:\n%s", err, out)
	}

	// Fixed: noma start resumes at the failed command.
	sh("touch fixed")
	if out, err := noma(t, "start", "e2e-setup"); err != nil || !strings.Contains(out, "is ready") {
		t.Fatalf("noma start after the fix: %v\n%s", err, out)
	}
	// A list that ran in full is skipped.
	if out, err := restart(); err != nil || !strings.Contains(out, "is ready") {
		t.Fatalf("restart: %v\n%s", err, out)
	}
	if got := sh("cat setup.log"); got != "ran\n" {
		t.Fatalf("setup.log is %q: each command should have run once", got)
	}

	// A changed list runs from the start.
	sh(`printf '  - echo changed >> setup.log\n' >> noma.yaml`)
	if out, err := restart(); err != nil || !strings.Contains(out, "is ready") {
		t.Fatalf("restart with a changed list: %v\n%s", err, out)
	}
	if got := sh("cat setup.log"); got != "ran\nran\nchanged\n" {
		t.Fatalf("setup.log is %q after the list changed", got)
	}
}
