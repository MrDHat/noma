package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLink(t *testing.T) {
	for in, want := range map[string]string{
		"session_018F657vgSxPwx7bqumUykhw": "https://claude.ai/code/session_018F657vgSxPwx7bqumUykhw",
		"cse_018F657vgSxPwx7bqumUykhw":     "https://claude.ai/code/session_018F657vgSxPwx7bqumUykhw",
		"env_01PakQY8crRdTwvnRqw2vwxC":     "",
		"session_x/../y":                   "",
		"":                                 "",
	} {
		if got := Link(in); got != want {
			t.Errorf("Link(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSessionLinkTakesOnlyThisRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge-pointer.json")
	pointer := `{"sessionId":"session_abc123","environmentId":"env_xyz","pid":56}`
	if err := os.WriteFile(path, []byte(pointer), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := sessionLink(path, 56); got != "https://claude.ai/code/session_abc123" {
		t.Errorf("pointer from this run gave %q", got)
	}
	if got := sessionLink(path, 57); got != "" {
		t.Errorf("pointer from an earlier run gave %q, want none", got)
	}
}

func TestSetAccount(t *testing.T) {
	c := &Claude{Home: t.TempDir(), Dir: "/w"}
	if err := c.Seed(); err != nil {
		t.Fatal(err)
	}
	var p profile
	p.Account.UUID, p.Account.Email = "acct-1", "a@example.com"
	if err := c.SetAccount(p); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(c.Home, ".claude.json")); strings.Contains(string(b), "oauthAccount") {
		t.Fatal("wrote an account without an organization")
	}
	p.Organization.UUID, p.Organization.Name = "org-1", "Org"
	if err := c.SetAccount(p); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(c.Home, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Onboarded bool              `json:"hasCompletedOnboarding"`
		Account   map[string]string `json:"oauthAccount"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Onboarded || cfg.Account["accountUuid"] != "acct-1" || cfg.Account["organizationUuid"] != "org-1" {
		t.Errorf("got %s", b)
	}
}
