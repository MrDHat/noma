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
