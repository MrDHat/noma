package server

import (
	"strings"
	"testing"

	"github.com/MrDHat/noma/internal/api"
)

func TestNameFromRepo(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/foo/Bar.git":                                 "bar",
		"https://github.com/foo/bar/":                                    "bar",
		"git@github.com:foo/my_repo.git":                                 "my-repo",
		"https://example.com/x/a-very-long-repository-name-that-goes-on": "a-very-long-repository-name-that",
		"https://example.com/":                                           "workspace",
	} {
		if got := nameFromRepo(in); got != want {
			t.Errorf("nameFromRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidGitIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, email string
		want        bool
	}{
		{"Ada Lovelace", "ada@example.com", true},
		{"", "ada@example.com", false},
		{"Ada", "", false},
		{"Ada\n[core]", "ada@example.com", false},
		{"Ada", "ada@example.com\r", false},
		{strings.Repeat("a", 201), "ada@example.com", false},
	} {
		if got := validGitIdentity(&api.GitIdentity{Name: tc.name, Email: tc.email}); got != tc.want {
			t.Errorf("validGitIdentity(%q, %q) = %v, want %v", tc.name, tc.email, got, tc.want)
		}
	}
}
