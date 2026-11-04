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

func TestValidImage(t *testing.T) {
	for in, want := range map[string]bool{
		"debian":                         true,
		"noma-e2e-project:dev":           true,
		"ghcr.io/mrdhat/noma-dev:v0.3.0": true,
		"localhost:5000/team/img@sha256:" + strings.Repeat("a", 64): true,
		"":                  false,
		"Debian":            false,
		"debian; rm -rf /":  false,
		"https://ghcr.io/x": false,
		"img@sha256:short":  false,
	} {
		if got := api.ValidImage(in); got != want {
			t.Errorf("ValidImage(%q) = %v, want %v", in, got, want)
		}
	}
}
