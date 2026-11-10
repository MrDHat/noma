package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

// A changed secret reaches Claude without a change message, and after a
// failed fetch: the cases the secret-delivery model found.
func TestCredentialsRefreshWithoutMessage(t *testing.T) {
	old := credentialRefresh
	credentialRefresh = 10 * time.Millisecond
	t.Cleanup(func() { credentialRefresh = old })

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agent/v1/credentials" {
			http.NotFound(w, r)
			return
		}
		if calls.Add(1) == 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(api.CredentialList{Credentials: []api.Credential{{Name: "k", Env: "API_KEY", Value: "new"}}})
	}))
	t.Cleanup(srv.Close)
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("t"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := &Agent{
		Client:             &Client{BaseURL: srv.URL, TokenFile: token, HTTP: srv.Client()},
		credentialsChanged: make(chan struct{}, 1),
		restartClaude:      make(chan struct{}, 1),
	}
	a.vault.set([]api.Credential{{Name: "k", Env: "API_KEY", Value: "old"}})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go a.keepCredentials(ctx)

	select {
	case <-a.restartClaude:
	case <-time.After(5 * time.Second):
		t.Fatalf("Claude was not restarted; %d fetches", calls.Load())
	}
	if got := a.vault.env(); len(got) != 1 || got[0] != "API_KEY=new" {
		t.Fatalf("vault env = %v", got)
	}
}
