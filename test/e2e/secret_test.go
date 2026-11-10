//go:build e2e

package e2e

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

func TestSecretsAreStoredOnlyEncrypted(t *testing.T) {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	value := "ghp_e2e" + hex.EncodeToString(b)

	out, err := nomaWithInput(t, value+"\n", "secret", "set", "e2e-gh", "--env", "GH_TOKEN", "--git-host", "github.com", "--repos", "github.com/octocat/*")
	t.Cleanup(func() { _, _ = noma(t, "secret", "rm", "e2e-gh") })
	if err != nil {
		t.Fatalf("noma secret set: %v\n%s", err, out)
	}
	out = mustNoma(t, "secret", "ls")
	if !strings.Contains(out, "e2e-gh") || !strings.Contains(out, "git:github.com,env:GH_TOKEN") || !strings.Contains(out, "github.com/octocat/*") {
		t.Fatalf("noma secret ls lacks the secret's delivery:\n%s", out)
	}
	if strings.Contains(out, value) {
		t.Fatal("noma secret ls shows the value")
	}

	dump := kubectl(t, "-n", "noma-system", "exec", "postgres-0", "--", "pg_dump", "-U", "noma", "noma")
	logs := kubectl(t, "-n", "noma-system", "logs", "deploy/noma-server")
	for what, text := range map[string]string{"database": dump, "server logs": logs} {
		if strings.Contains(text, value) {
			t.Errorf("the secret's value appears in the %s", what)
		}
	}
	if !strings.Contains(dump, "e2e-gh") {
		t.Error("the database has no row for the secret")
	}

	if out, err := nomaWithInput(t, "x\n", "secret", "set", "e2e-bad", "--env", "LD_PRELOAD"); err == nil {
		t.Fatalf("a secret for LD_PRELOAD was accepted:\n%s", out)
	}
	mustNoma(t, "secret", "rm", "e2e-gh")
	if out := mustNoma(t, "secret", "ls"); strings.Contains(out, "e2e-gh") {
		t.Fatalf("secret still listed after rm:\n%s", out)
	}
	if _, err := noma(t, "secret", "rm", "e2e-gh"); err == nil {
		t.Fatal("removing a missing secret succeeded")
	}
}
