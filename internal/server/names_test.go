package server

import "testing"

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
