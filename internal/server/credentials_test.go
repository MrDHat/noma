package server

import "testing"

func TestRepoMatches(t *testing.T) {
	tests := []struct {
		patterns []string
		repo     string
		want     bool
	}{
		{nil, "https://github.com/acme/api", true},
		{[]string{"github.com/acme/*"}, "https://github.com/acme/api", true},
		{[]string{"github.com/acme/*"}, "https://github.com/acme/api.git", true},
		{[]string{"github.com/acme/*"}, "https://GitHub.com/acme/api/", true},
		{[]string{"github.com/acme/*"}, "https://github.com/other/api", false},
		{[]string{"github.com/acme/*"}, "https://github.com/acme/team/api", false},
		{[]string{"github.com/acme/*"}, "https://gitlab.com/acme/api", false},
		{[]string{"github.com/acme/api"}, "https://github.com/acme/api", true},
		{[]string{"github.com/acme/api"}, "https://github.com/acme/api-two", false},
		{[]string{"gitlab.com/x/*", "github.com/acme/*"}, "https://github.com/acme/api", true},
		{[]string{"github.com/acme/*"}, "not a url", false},
	}
	for _, tt := range tests {
		if got := repoMatches(tt.patterns, tt.repo); got != tt.want {
			t.Errorf("repoMatches(%q, %q) = %v, want %v", tt.patterns, tt.repo, got, tt.want)
		}
	}
}
