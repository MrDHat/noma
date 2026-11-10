package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/MrDHat/noma/internal/api"
	"github.com/MrDHat/noma/internal/secrets"
	"github.com/MrDHat/noma/internal/store"
)

// A CredentialSource yields the credentials that reach a workspace. M3 has
// one source, values stored in Noma; external stores and minted tokens are
// further sources behind the same interface (M3-10).
type CredentialSource interface {
	Credentials(ctx context.Context, ws *store.Workspace) ([]api.Credential, error)
}

// StoredCredentials is the source for secrets set with `noma secret set`.
type StoredCredentials struct {
	Store *store.Store
	Key   *secrets.Key
}

// Credentials returns the owner's secrets whose rules match the workspace's
// repo, decrypted.
func (s StoredCredentials) Credentials(ctx context.Context, ws *store.Workspace) ([]api.Credential, error) {
	rows, err := s.Store.Credentials(ctx, store.OwnerUser, ws.Owner)
	if err != nil {
		return nil, err
	}
	var out []api.Credential
	for _, c := range rows {
		if !repoMatches(c.Repos, ws.RepoURL) {
			continue
		}
		value, err := s.Key.Open(c.KeyID, c.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("open secret %s: %w", c.Name, err)
		}
		out = append(out, api.Credential{Name: c.Name, GitHost: c.GitHost, Env: c.EnvVar, Value: string(value)})
	}
	return out, nil
}

// repoMatches says whether a secret limited to patterns reaches a workspace
// on repoURL. No patterns means every workspace. A pattern like
// github.com/acme/* matches the repos directly under acme: * stops at /.
func repoMatches(patterns []string, repoURL string) bool {
	if len(patterns) == 0 {
		return true
	}
	u, err := url.Parse(repoURL)
	if err != nil || u.Host == "" {
		return false
	}
	repo := strings.ToLower(u.Host) + strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), ".git")
	for _, p := range patterns {
		if ok, _ := path.Match(p, repo); ok {
			return true
		}
	}
	return false
}

// credentials returns the workspace's credentials. Only its own agent can
// ask, and the values are never logged.
func (a *AgentServer) credentials(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	list := api.CredentialList{Credentials: []api.Credential{}}
	if a.Credentials != nil {
		cs, err := a.Credentials.Credentials(r.Context(), ws)
		if err != nil {
			internalError(w, err)
			return
		}
		list.Credentials = append(list.Credentials, cs...)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, list)
}
