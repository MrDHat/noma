// Package server is noma-server's HTTP API.
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/MrDHat/noma/internal/api"
	"github.com/MrDHat/noma/internal/store"
)

// devUser is the only user until real login arrives (M5).
const devUser = "dev"

// Server serves the CLI API.
type Server struct {
	Store      *store.Store
	AdminToken string
}

// Handler returns the HTTP handler for the CLI API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("POST /v1/workspaces", s.authed(s.createWorkspace))
	mux.Handle("GET /v1/workspaces/{name}", s.authed(s.getWorkspace))
	mux.Handle("DELETE /v1/workspaces/{name}", s.authed(s.deleteWorkspace))
	return mux
}

// authed checks the static admin token and passes the user on.
func (s *Server) authed(h func(w http.ResponseWriter, r *http.Request, user string)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(s.AdminToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or invalid token")
			return
		}
		h(w, r, devUser)
	})
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request, user string) {
	var req api.CreateWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if u, err := url.Parse(req.RepoURL); err != nil || u.Scheme != "https" || u.Host == "" {
		writeError(w, http.StatusBadRequest, "repoUrl must be an https git URL")
		return
	}
	if req.Name != "" && !validName.MatchString(req.Name) {
		writeError(w, http.StatusBadRequest, "name must be 1-40 lowercase letters, digits or dashes")
		return
	}
	ws, err := s.insertWorkspace(r.Context(), user, req)
	if errors.Is(err, store.ErrNameTaken) {
		writeError(w, http.StatusConflict, fmt.Sprintf("workspace %q already exists", req.Name))
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPI(ws))
}

// insertWorkspace stores a new workspace. Without an explicit name it uses
// the repo name, adding -2, -3, … until one is free.
func (s *Server) insertWorkspace(ctx context.Context, user string, req api.CreateWorkspaceRequest) (*store.Workspace, error) {
	base := req.Name
	if base == "" {
		base = nameFromRepo(req.RepoURL)
	}
	for i := 1; i <= 100; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		ws := &store.Workspace{
			ID:      newID(),
			Owner:   user,
			Name:    name,
			RepoURL: req.RepoURL,
			Desired: store.DesiredRunning,
			Phase:   api.PhasePending,
		}
		err := s.Store.CreateWorkspace(ctx, ws)
		if errors.Is(err, store.ErrNameTaken) && req.Name == "" {
			continue
		}
		return ws, err
	}
	return nil, fmt.Errorf("no free name for %q", base)
}

func (s *Server) getWorkspace(w http.ResponseWriter, r *http.Request, user string) {
	ws, err := s.Store.WorkspaceByName(r.Context(), user, r.PathValue("name"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPI(ws))
}

func (s *Server) deleteWorkspace(w http.ResponseWriter, r *http.Request, user string) {
	ws, err := s.Store.WorkspaceByName(r.Context(), user, r.PathValue("name"))
	if err == nil {
		err = s.Store.DeleteWorkspace(r.Context(), ws.ID)
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toAPI(ws *store.Workspace) api.Workspace {
	return api.Workspace{
		ID:        ws.ID,
		Name:      ws.Name,
		RepoURL:   ws.RepoURL,
		Phase:     ws.Phase,
		Message:   ws.Message,
		CreatedAt: ws.CreatedAt,
	}
}

// newID returns a workspace ID that is also a valid Kubernetes object name.
func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "ws-" + hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, api.Error{Error: msg})
}

func internalError(w http.ResponseWriter, err error) {
	slog.Error("request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
