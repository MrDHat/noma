package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/MrDHat/noma/internal/api"
	"github.com/MrDHat/noma/internal/store"
)

var (
	secretName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)
	gitHost    = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?(:[0-9]{1,5})?$`)
	envVar     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	// repoPattern is a host and path, like github.com/acme/* or
	// github.com/acme/api.
	repoPattern = regexp.MustCompile(`^[a-z0-9-]+(\.[a-z0-9-]+)+(:[0-9]{1,5})?(/[A-Za-z0-9._*-]+)+$`)
)

// reservedEnv are variables a secret may not set: they change how the
// shell, git, Claude or the agent behave, or run commands.
var reservedEnv = []string{"PATH", "HOME", "USER", "SHELL", "TERM", "PWD", "ENV", "BASH_ENV", "LD_", "GIT_", "CLAUDE_", "NOMA_"}

const maxSecretValue = 8 << 10

func validEnvVar(name string) bool {
	if !envVar.MatchString(name) {
		return false
	}
	upper := strings.ToUpper(name)
	for _, r := range reservedEnv {
		if upper == r || (strings.HasSuffix(r, "_") && strings.HasPrefix(upper, r)) {
			return false
		}
	}
	return true
}

// checkSecret returns what is wrong with a secret, or "".
func checkSecret(name string, req *api.SecretRequest) string {
	switch {
	case !secretName.MatchString(name):
		return "name must be up to 63 letters, digits, dots, dashes or underscores"
	case req.Value == "" || len(req.Value) > maxSecretValue || strings.ContainsRune(req.Value, 0):
		return fmt.Sprintf("value must be 1 to %d bytes", maxSecretValue)
	case req.GitHost == "" && req.Env == "":
		return "say how to deliver it: --git-host, --env or both"
	case req.GitHost != "" && !gitHost.MatchString(req.GitHost):
		return "git host must be a host name, like github.com"
	case req.Env != "" && !validEnvVar(req.Env):
		return "env must be a variable name that doesn't change how the shell, git, Claude or Noma behave"
	case len(req.Repos) > 20:
		return "at most 20 repo patterns"
	}
	for _, p := range req.Repos {
		if !repoPattern.MatchString(p) {
			return fmt.Sprintf("repo pattern %q must look like github.com/acme/*", p)
		}
	}
	return ""
}

func (s *Server) putSecret(w http.ResponseWriter, r *http.Request, user string) {
	var req api.SecretRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	name := r.PathValue("name")
	if msg := checkSecret(name, &req); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	sealed, err := s.SecretKey.Seal([]byte(req.Value))
	if err != nil {
		internalError(w, err)
		return
	}
	err = s.Store.PutCredential(r.Context(), &store.Credential{
		ID: "cred-" + newID()[3:], OwnerKind: store.OwnerUser, Owner: user, Name: name,
		GitHost: req.GitHost, EnvVar: req.Env, Repos: req.Repos,
		KeyID: s.SecretKey.ID, Ciphertext: sealed,
	})
	if err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listSecrets(w http.ResponseWriter, r *http.Request, user string) {
	creds, err := s.Store.Credentials(r.Context(), store.OwnerUser, user)
	if err != nil {
		internalError(w, err)
		return
	}
	list := api.SecretList{Secrets: make([]api.Secret, 0, len(creds))}
	for _, c := range creds {
		list.Secrets = append(list.Secrets, api.Secret{
			Name: c.Name, GitHost: c.GitHost, Env: c.EnvVar, Repos: c.Repos, UpdatedAt: c.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) deleteSecret(w http.ResponseWriter, r *http.Request, user string) {
	err := s.Store.DeleteCredential(r.Context(), store.OwnerUser, user, r.PathValue("name"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "secret not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
