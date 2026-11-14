package server

import (
	"encoding/json"
	"net/http"

	"github.com/MrDHat/noma/internal/api"
)

// putClaudeSetup stores the user's Claude setup; their workspaces copy it
// into ~/.claude when they next start (M3.5-5, M3.5-13).
func (s *Server) putClaudeSetup(w http.ResponseWriter, r *http.Request, user string) {
	var req api.ClaudeSetup
	// The archive is base64 in JSON: a third bigger, plus room for the rest.
	limit := int64(api.MaxClaudeSetup)*4/3 + 1024
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid or too large Claude setup")
		return
	}
	if len(req.Archive) > api.MaxClaudeSetup {
		writeError(w, http.StatusBadRequest, "Claude setup too large")
		return
	}
	if req.Archive == nil {
		req.Archive = []byte{}
	}
	if err := s.Store.PutClaudeSetup(r.Context(), user, req.Archive); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
