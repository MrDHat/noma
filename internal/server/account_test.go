package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFakeClaudeProfile(t *testing.T) {
	for auth, want := range map[string]string{
		"Bearer fake-token-fake-account": `"uuid":"fake-account"`,
		"Bearer fake-token-":             "invalid token",
		"Bearer real-token":              "invalid token",
		"":                               "invalid token",
	} {
		req := httptest.NewRequest(http.MethodGet, "/fake/claude/profile", nil)
		req.Header.Set("Authorization", auth)
		rec := httptest.NewRecorder()
		fakeClaudeProfile(rec, req)
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%q: got %d %s, want %s", auth, rec.Code, rec.Body, want)
		}
	}
}
