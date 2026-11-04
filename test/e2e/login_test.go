//go:build e2e

package e2e

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// sharedCredentials is the Claude login every workspace of the user shares.
const sharedCredentials = "/var/lib/noma/claude-login/.credentials.json"

// signOut removes the user's shared Claude login, so the next workspace asks
// to sign in. It runs a short-lived pod on the login's volume.
func signOut(t *testing.T) {
	t.Helper()
	if out, err := exec.Command("kubectl", "--context", kubeContext(), "-n", userNamespace,
		"get", "pvc", "claude-login").CombinedOutput(); err != nil {
		if strings.Contains(string(out), "NotFound") {
			return // nobody has signed in yet
		}
		t.Fatalf("get the login volume: %v\n%s", err, out)
	}
	overrides := `{"spec":{
	  "securityContext":{"runAsNonRoot":true,"runAsUser":1000,"runAsGroup":1000,"fsGroup":1000,"seccompProfile":{"type":"RuntimeDefault"}},
	  "containers":[{"name":"signout","image":"noma-workspace:dev","imagePullPolicy":"IfNotPresent",
	    "command":["rm","-f","` + sharedCredentials + `"],
	    "securityContext":{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]}},
	    "volumeMounts":[{"name":"login","mountPath":"/var/lib/noma/claude-login"}]}],
	  "volumes":[{"name":"login","persistentVolumeClaim":{"claimName":"claude-login"}}]}}`
	kubectl(t, "-n", userNamespace, "run", "e2e-signout", "--rm", "-i", "--quiet", "--restart=Never",
		"--image=noma-workspace:dev", "--overrides="+overrides)
}

// Demo step 1: the user signs in once; every other workspace starts signed
// in, and deleting the first one keeps the login.
func TestWorkspacesShareOneLogin(t *testing.T) {
	signOut(t)
	out := mustNoma(t, "up", "--name", "e2e-login-a", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-login-a") })
	if n := strings.Count(out, "Paste code:"); n != 1 {
		t.Fatalf("the first workspace asked for a code %d times, want once:\n%s", n, out)
	}
	out, err := nomaWithInput(t, "", "up", "--name", "e2e-login-b", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-login-b") })
	if err != nil || strings.Contains(out, "Paste code:") {
		t.Fatalf("the second workspace should start signed in; err %v:\n%s", err, out)
	}

	mustNoma(t, "rm", "e2e-login-a")
	ws, err := client(t).Workspace(context.Background(), "e2e-login-b")
	if err != nil {
		t.Fatal(err)
	}
	kubectl(t, "-n", userNamespace, "exec", ws.ID+"-0", "--", "test", "-f", sharedCredentials)
	mustNoma(t, "stop", "e2e-login-b")
	if out, err := nomaWithInput(t, "", "start", "e2e-login-b"); err != nil || strings.Contains(out, "Paste code:") {
		t.Fatalf("e2e-login-b should start signed in after e2e-login-a is gone; err %v:\n%s", err, out)
	}
}

// Two workspaces started together need only one code: once one signs in,
// the other stops asking.
func TestConcurrentUpsNeedOneCode(t *testing.T) {
	signOut(t)
	t.Cleanup(func() {
		_, _ = noma(t, "rm", "e2e-login-c")
		_, _ = noma(t, "rm", "e2e-login-d")
	})
	// d's user never types anything; its stdin stays open.
	stdin, never := io.Pipe()
	defer never.Close() //nolint:errcheck
	d := exec.Command(nomaBin, "up", "--name", "e2e-login-d", demoRepo)
	d.Stdin = stdin
	d.Env = append(os.Environ(), "NOMA_NO_BROWSER=1")
	var dOut strings.Builder
	d.Stdout, d.Stderr = &dOut, &dOut
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	var dErr error
	go func() { defer wg.Done(); dErr = d.Wait() }()

	// Give d time to reach its sign-in prompt first.
	time.Sleep(10 * time.Second)
	mustNoma(t, "up", "--name", "e2e-login-c", demoRepo)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Minute):
		_ = d.Process.Kill()
		<-done
		t.Fatalf("noma up for e2e-login-d kept waiting after e2e-login-c signed in:\n%s", dOut.String())
	}
	if dErr != nil || !linkPattern.MatchString(dOut.String()) {
		t.Fatalf("noma up for e2e-login-d: %v\n%s", dErr, dOut.String())
	}
	t.Logf("e2e-login-d:\n%s", dOut.String())
}
