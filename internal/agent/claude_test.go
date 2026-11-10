package agent

import (
	"os"
	"path/filepath"
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
