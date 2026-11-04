package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MrDHat/noma/internal/api"
)

func TestCredentialHelperAsksTheAgent(t *testing.T) {
	// Unix socket paths are short; t.TempDir can be too long on macOS.
	dir, err := os.MkdirTemp("", "noma")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	old := credentialSocket
	credentialSocket = filepath.Join(dir, "c.sock")
	t.Cleanup(func() { credentialSocket = old })

	a := &Agent{}
	a.vault.set([]api.Credential{
		{Name: "gh", GitHost: "github.com", Value: "tok-1"},
		{Name: "env-only", Env: "FOO", Value: "not-for-git"},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.serveCredentials(ctx); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		args  []string
		input string
		want  string
	}{
		{[]string{"get"}, "protocol=https\nhost=github.com\n\n", "username=x-access-token\npassword=tok-1\n"},
		{[]string{"get"}, "protocol=https\nhost=GitHub.com\npath=acme/api\n\n", "username=x-access-token\npassword=tok-1\n"},
		{[]string{"get"}, "protocol=https\nhost=gitlab.com\n\n", ""},
		{[]string{"get"}, "protocol=http\nhost=github.com\n\n", ""},
		{[]string{"store"}, "protocol=https\nhost=github.com\nusername=u\npassword=p\n\n", ""},
	}
	for _, tt := range tests {
		var out strings.Builder
		if err := CredentialHelper(tt.args, strings.NewReader(tt.input), &out); err != nil {
			t.Fatalf("%v %q: %v", tt.args, tt.input, err)
		}
		if out.String() != tt.want {
			t.Errorf("%v %q: got %q, want %q", tt.args, tt.input, out.String(), tt.want)
		}
	}
}

func TestVaultEnvAndClaudeEnv(t *testing.T) {
	var v vault
	v.set([]api.Credential{
		{Name: "b", Env: "B_TOKEN", Value: "2"},
		{Name: "git", GitHost: "github.com", Value: "not-env"},
		{Name: "a", Env: "A_TOKEN", Value: "1"},
	})
	if got, want := strings.Join(v.env(), ","), "A_TOKEN=1,B_TOKEN=2"; got != want {
		t.Fatalf("env() = %s, want %s", got, want)
	}
	c := &Claude{Bin: "true", Home: "/home/w", Env: func() []string { return []string{"A_TOKEN=1", "HOME=/evil"} }}
	env := c.command(context.Background()).Env
	last := map[string]string{}
	for _, kv := range env {
		k, val, _ := strings.Cut(kv, "=")
		last[k] = val
	}
	if last["A_TOKEN"] != "1" || last["HOME"] != "/home/w" {
		t.Fatalf("Claude's env: A_TOKEN=%q HOME=%q", last["A_TOKEN"], last["HOME"])
	}
}
