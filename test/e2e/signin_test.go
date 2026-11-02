//go:build e2e

package e2e

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

func TestSignInRelaysOnlyCiphertext(t *testing.T) {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	code := "fake-code-" + hex.EncodeToString(b)

	out, err := nomaWithInput(t, code+"\n", "up", "--name", "e2e-signin", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-signin") })
	if err != nil {
		t.Fatalf("noma up: %v\n%s", err, out)
	}
	for _, want := range []string{"oauth/authorize", "Paste code:", "is ready"} {
		if !strings.Contains(out, want) {
			t.Fatalf("noma up output lacks %q:\n%s", want, out)
		}
	}
	ws, err := client(t).Workspace(context.Background(), "e2e-signin")
	if err != nil {
		t.Fatal(err)
	}
	// Claude in the workspace got the code: it is logged in.
	kubectl(t, "-n", userNamespace, "exec", ws.ID+"-0", "--",
		"test", "-f", "/workspaces/"+ws.ID+"/home/.claude/.credentials.json")

	// The server and Postgres only ever saw it sealed.
	logs := kubectl(t, "-n", "noma-system", "logs", "deploy/noma-server")
	dump := kubectl(t, "-n", "noma-system", "exec", "postgres-0", "--", "pg_dump", "-U", "noma", "noma")
	for what, text := range map[string]string{"server logs": logs, "database": dump} {
		if strings.Contains(text, code) {
			t.Errorf("the sign-in code appears in the %s", what)
		}
	}
}

func TestWrongSignInCodeIsAskedAgain(t *testing.T) {
	out, err := nomaWithInput(t, "wrong\n"+signInCode+"\n", "up", "--name", "e2e-retry", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-retry") })
	if err != nil {
		t.Fatalf("noma up: %v\n%s", err, out)
	}
	if n := strings.Count(out, "Paste code:"); n != 2 {
		t.Fatalf("asked for the code %d times, want 2:\n%s", n, out)
	}
}
