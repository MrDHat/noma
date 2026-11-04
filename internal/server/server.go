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
	"time"

	"github.com/MrDHat/noma/internal/api"
	"github.com/MrDHat/noma/internal/store"
)

// devUser is the only user until real login arrives (M5).
const devUser = "dev"

// maxBody caps request bodies; every request the CLI sends is small.
const maxBody = 16 << 10

// Server serves the CLI API.
type Server struct {
	Store      *store.Store
	AdminToken string
	// Wake asks the reconciler to act on a change.
	Wake func()
	// SendToAgent queues a message for a workspace's agent.
	SendToAgent func(id string, m api.AgentMessage) error
}

// Handler returns the HTTP handler for the CLI API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("GET /v1/workspaces", s.authed(s.listWorkspaces))
	mux.Handle("POST /v1/workspaces", s.authed(s.createWorkspace))
	mux.Handle("GET /v1/workspaces/{name}", s.authed(s.getWorkspace))
	mux.Handle("DELETE /v1/workspaces/{name}", s.authed(s.deleteWorkspace))
	mux.Handle("POST /v1/workspaces/{name}/stop", s.authed(s.stopWorkspace))
	mux.Handle("POST /v1/workspaces/{name}/start", s.authed(s.startWorkspace))
	mux.Handle("POST /v1/workspaces/{name}/signin", s.authed(s.signIn))
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
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
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
	s.Wake()
	writeJSON(w, http.StatusCreated, toAPI(ws))
}

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request, user string) {
	rows, err := s.Store.WorkspacesOf(r.Context(), user)
	if err != nil {
		internalError(w, err)
		return
	}
	list := api.WorkspaceList{Workspaces: make([]api.Workspace, 0, len(rows))}
	for i := range rows {
		list.Workspaces = append(list.Workspaces, toAPI(&rows[i]))
	}
	writeJSON(w, http.StatusOK, list)
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
		err = s.Store.MarkDeleted(r.Context(), ws.ID)
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	s.Wake()
	w.WriteHeader(http.StatusAccepted)
}

// stopWorkspace stops a running workspace, keeping its volume. Stopping a
// stopped workspace does nothing.
func (s *Server) stopWorkspace(w http.ResponseWriter, r *http.Request, user string) {
	ws, ok := s.workspace(w, r, user)
	if !ok {
		return
	}
	if ws.Desired == store.DesiredDeleted {
		writeError(w, http.StatusConflict, "workspace is being deleted")
		return
	}
	if err := s.Store.MarkStopped(r.Context(), ws.ID); err != nil {
		internalError(w, err)
		return
	}
	s.Wake()
	w.WriteHeader(http.StatusAccepted)
}

// startWorkspace starts a stopped workspace on its volume. Starting a
// running workspace does nothing; one still stopping is refused.
func (s *Server) startWorkspace(w http.ResponseWriter, r *http.Request, user string) {
	ws, ok := s.workspace(w, r, user)
	if !ok {
		return
	}
	switch ws.Desired {
	case store.DesiredRunning:
		w.WriteHeader(http.StatusAccepted)
		return
	case store.DesiredDeleted:
		writeError(w, http.StatusConflict, "workspace is being deleted")
		return
	}
	err := s.Store.MarkStarted(r.Context(), ws.ID)
	if errors.Is(err, store.ErrNotStopped) {
		writeError(w, http.StatusConflict, "workspace is still stopping; try again once it is stopped")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	s.Wake()
	w.WriteHeader(http.StatusAccepted)
}

// workspace looks up the user's workspace named in the path, writing the
// error response if it can't.
func (s *Server) workspace(w http.ResponseWriter, r *http.Request, user string) (*store.Workspace, bool) {
	ws, err := s.Store.WorkspaceByName(r.Context(), user, r.PathValue("name"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "workspace not found")
		return nil, false
	}
	if err != nil {
		internalError(w, err)
		return nil, false
	}
	return ws, true
}

// signIn passes a sealed Claude sign-in code to the workspace's agent. The
// server can't read it and never stores it.
func (s *Server) signIn(w http.ResponseWriter, r *http.Request, user string) {
	var req api.SignInCode
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil || req.SealedCode == "" {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	ws, err := s.Store.WorkspaceByName(r.Context(), user, r.PathValue("name"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if ws.Phase != api.PhaseSignIn || ws.SignInKey == "" || ws.SignInKey != req.PublicKey {
		writeError(w, http.StatusConflict, "workspace is not waiting for this sign-in")
		return
	}
	if err := s.SendToAgent(ws.ID, api.AgentMessage{Type: api.MessageSignInCode, SignInCode: &req}); err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func toAPI(ws *store.Workspace) api.Workspace {
	var signIn *api.SignIn
	if ws.Phase == api.PhaseSignIn && ws.SignInURL != "" {
		signIn = &api.SignIn{URL: ws.SignInURL, PublicKey: ws.SignInKey}
	}
	return api.Workspace{
		SignIn:    signIn,
		ID:        ws.ID,
		Name:      ws.Name,
		RepoURL:   ws.RepoURL,
		Phase:     ws.Phase,
		Message:   ws.Message,
		Link:      ws.Link,
		CreatedAt: ws.CreatedAt,

		AgentSilentSeconds: int(agentSilence(ws, time.Now()).Seconds()),
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
