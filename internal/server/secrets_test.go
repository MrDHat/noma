package server

import (
	"strings"
	"testing"

	"github.com/MrDHat/noma/internal/api"
)

func TestCheckSecret(t *testing.T) {
	ok := func() api.SecretRequest { return api.SecretRequest{Value: "v", Env: "GH_TOKEN"} }
	for _, tc := range []struct {
		name   string
		secret string
		edit   func(*api.SecretRequest)
		bad    bool
	}{
		{"env", "gh", func(*api.SecretRequest) {}, false},
		{"git host and repos", "gh", func(r *api.SecretRequest) {
			r.Env, r.GitHost, r.Repos = "", "github.com", []string{"github.com/acme/*", "gitlab.example.com:8443/x/y"}
		}, false},
		{"no delivery", "gh", func(r *api.SecretRequest) { r.Env = "" }, true},
		{"empty value", "gh", func(r *api.SecretRequest) { r.Value = "" }, true},
		{"huge value", "gh", func(r *api.SecretRequest) { r.Value = strings.Repeat("x", maxSecretValue+1) }, true},
		{"bad name", "../x", func(*api.SecretRequest) {}, true},
		{"bad host", "gh", func(r *api.SecretRequest) { r.GitHost = "https://github.com" }, true},
		{"bad repo", "gh", func(r *api.SecretRequest) { r.Repos = []string{"acme/*"} }, true},
		{"PATH", "gh", func(r *api.SecretRequest) { r.Env = "PATH" }, true},
		{"LD_PRELOAD", "gh", func(r *api.SecretRequest) { r.Env = "LD_PRELOAD" }, true},
		{"GIT_SSH_COMMAND", "gh", func(r *api.SecretRequest) { r.Env = "git_ssh_command" }, true},
		{"CLAUDE_*", "gh", func(r *api.SecretRequest) { r.Env = "CLAUDE_SECURESTORAGE_CONFIG_DIR" }, true},
		{"not a name", "gh", func(r *api.SecretRequest) { r.Env = "A B" }, true},
	} {
		req := ok()
		tc.edit(&req)
		if got := checkSecret(tc.secret, &req); (got != "") != tc.bad {
			t.Errorf("%s: checkSecret = %q, want bad=%v", tc.name, got, tc.bad)
		}
	}
}
