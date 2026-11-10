package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/MrDHat/noma/internal/api"
	"github.com/MrDHat/noma/internal/store"
)

// claudeAccount is the shape of a Claude account ID, a UUID in practice.
var claudeAccount = regexp.MustCompile(`^[A-Za-z0-9-]{1,100}$`)

// account checks the Claude account a workspace's login belongs to against
// the one pinned for its user, pinning it if it is the first (M3-26). The
// login is shared by the user's workspaces, so a workspace that planted
// another account's token would otherwise send every workspace's work to it.
func (a *AgentServer) account(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	var body api.AgentAccount
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil || !claudeAccount.MatchString(body.Account) {
		writeError(w, http.StatusBadRequest, "invalid account")
		return
	}
	ok, err := a.Store.PinClaudeAccount(r.Context(), ws.Owner, body.Account)
	if err != nil {
		internalError(w, err)
		return
	}
	if !ok {
		slog.Warn("claude account changed", "workspace", ws.ID, "owner", ws.Owner)
		writeError(w, http.StatusConflict, "Claude account changed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fakeClaudeProfile stands in for Claude's profile endpoint in CI. The fake
// Claude's tokens look like fake-token-<account>.
func fakeClaudeProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer fake-token-")
	if !ok || !claudeAccount.MatchString(id) {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": map[string]string{"uuid": id}})
}
