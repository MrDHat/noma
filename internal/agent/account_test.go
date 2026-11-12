package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAccessToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_SECURESTORAGE_CONFIG_DIR", "")
	write := func(body string) {
		t.Helper()
		dir := filepath.Join(home, ".claude")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := accessToken(home); err == nil {
		t.Fatal("no credentials file, but no error")
	}
	later := time.Now().Add(time.Hour).UnixMilli()
	write(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"tok","expiresAt":%d}}`, later))
	if tok, fresh, err := accessToken(home); err != nil || tok != "tok" || !fresh {
		t.Fatalf("fresh token: %q %v %v", tok, fresh, err)
	}
	write(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"tok","expiresAt":%d}}`, time.Now().Add(-time.Hour).UnixMilli()))
	if _, fresh, err := accessToken(home); err != nil || fresh {
		t.Fatalf("expired token: fresh %v, err %v", fresh, err)
	}
	write(`{"fake":true}`)
	if _, _, err := accessToken(home); err == nil {
		t.Fatal("credentials without a token, but no error")
	}

	shared := t.TempDir()
	t.Setenv("CLAUDE_SECURESTORAGE_CONFIG_DIR", shared)
	if err := os.WriteFile(filepath.Join(shared, ".credentials.json"), fmt.Appendf(nil, `{"claudeAiOauth":{"accessToken":"shared","expiresAt":%d}}`, later), 0o600); err != nil {
		t.Fatal(err)
	}
	if tok, _, _ := accessToken(home); tok != "shared" {
		t.Fatalf("token %q, want the one in CLAUDE_SECURESTORAGE_CONFIG_DIR", tok)
	}
}

func TestClaudeAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer good":
			_, _ = w.Write([]byte(`{"account":{"uuid":"acct-1"},"organization":{"uuid":"org-1"}}`))
		case "Bearer inference-only":
			w.WriteHeader(http.StatusForbidden)
		case "Bearer flaky":
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()
	a := &Agent{ProfileURL: srv.URL}

	if p, err := a.claudeAccount(context.Background(), "good"); err != nil || p.Account.UUID != "acct-1" || p.Organization.UUID != "org-1" {
		t.Fatalf("good token: %+v %v", p, err)
	}
	for _, tok := range []string{"inference-only", "revoked"} {
		if _, err := a.claudeAccount(context.Background(), tok); !errors.Is(err, errAccountChanged) {
			t.Errorf("%s: err %v, want errAccountChanged", tok, err)
		}
	}
	if _, err := a.claudeAccount(context.Background(), "flaky"); err == nil || errors.Is(err, errAccountChanged) {
		t.Errorf("a 502 should be an error that doesn't block Claude, got %v", err)
	}
}
