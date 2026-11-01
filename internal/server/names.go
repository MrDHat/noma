package server

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

var (
	nameInvalid = regexp.MustCompile(`[^a-z0-9-]+`)
	validName   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)
)

// nameFromRepo derives a workspace name from a git URL:
// https://github.com/foo/Bar.git becomes "bar".
func nameFromRepo(repoURL string) string {
	p := repoURL
	if u, err := url.Parse(repoURL); err == nil && u.Path != "" {
		p = u.Path
	}
	base := strings.TrimSuffix(path.Base(strings.TrimRight(p, "/")), ".git")
	name := strings.Trim(nameInvalid.ReplaceAllString(strings.ToLower(base), "-"), "-")
	if len(name) > 32 {
		name = strings.Trim(name[:32], "-")
	}
	if name == "" {
		return "workspace"
	}
	return name
}
