package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	authnv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/MrDHat/noma/internal/api"
	"github.com/MrDHat/noma/internal/store"
)

// longPoll is how long GET /agent/v1/messages waits before answering empty.
const longPoll = 30 * time.Second

// seenEvery is how often an agent's polls are recorded; silentAfter is how
// long after the last one ls and attach say the agent has gone quiet. An
// agent polls at least every longPoll, so a healthy one is never more than
// seenEvery+longPoll behind.
const (
	seenEvery   = time.Minute
	silentAfter = 2 * time.Minute
)

// AgentServer serves the in-cluster endpoint that workspace agents dial.
type AgentServer struct {
	Store *store.Store
	Kube  kubernetes.Interface
	// Wake asks the reconciler to act on a change, such as a new image.
	Wake func()
	// Credentials yields the credentials each workspace gets.
	Credentials CredentialSource
	// FakeClaude serves a stand-in for Claude's profile endpoint, for
	// workspaces running the fake Claude in CI.
	FakeClaude bool

	mu    sync.Mutex
	boxes map[string]chan api.AgentMessage
}

// Handler returns the HTTP handler for agents.
func (a *AgentServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /agent/v1/workspace", a.authed(a.config))
	mux.Handle("POST /agent/v1/status", a.authed(a.status))
	mux.Handle("GET /agent/v1/messages", a.authed(a.messages))
	mux.Handle("GET /agent/v1/credentials", a.authed(a.credentials))
	mux.Handle("POST /agent/v1/account", a.authed(a.account))
	mux.Handle("GET /agent/v1/claude-setup", a.authed(a.claudeSetup))
	if a.FakeClaude {
		mux.HandleFunc("GET /fake/claude/profile", fakeClaudeProfile)
	}
	return mux
}

// Send queues a message for a workspace's agent. It fails if the agent's
// queue is full rather than block the caller.
func (a *AgentServer) Send(id string, m api.AgentMessage) error {
	select {
	case a.box(id) <- m:
		return nil
	default:
		return fmt.Errorf("agent for %s is not picking up messages", id)
	}
}

func (a *AgentServer) box(id string) chan api.AgentMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.boxes == nil {
		a.boxes = map[string]chan api.AgentMessage{}
	}
	b, ok := a.boxes[id]
	if !ok {
		b = make(chan api.AgentMessage, 8)
		a.boxes[id] = b
	}
	return b
}

// authed checks the agent's Kubernetes token with a TokenReview and maps it
// to the workspace whose ServiceAccount and pod it was issued for. The agent
// never names its workspace itself.
func (a *AgentServer) authed(h func(w http.ResponseWriter, r *http.Request, ws *store.Workspace)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := a.workspaceForToken(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			slog.Warn("agent request refused", "err", err)
			writeError(w, http.StatusUnauthorized, "invalid workspace token")
			return
		}
		h(w, r, ws)
	})
}

func (a *AgentServer) workspaceForToken(ctx context.Context, header string) (*store.Workspace, error) {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || token == "" {
		return nil, errors.New("no bearer token")
	}
	review, err := a.Kube.AuthenticationV1().TokenReviews().Create(ctx, &authnv1.TokenReview{
		Spec: authnv1.TokenReviewSpec{Token: token, Audiences: []string{api.TokenAudience}},
	}, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("token review: %w", err)
	}
	if !review.Status.Authenticated {
		return nil, fmt.Errorf("token not authenticated: %s", review.Status.Error)
	}
	// system:serviceaccount:noma-u-<owner>:<workspace id>
	parts := strings.Split(review.Status.User.Username, ":")
	if len(parts) != 4 || parts[0] != "system" || parts[1] != "serviceaccount" {
		return nil, fmt.Errorf("not a service account: %q", review.Status.User.Username)
	}
	owner, ok := strings.CutPrefix(parts[2], "noma-u-")
	if !ok {
		return nil, fmt.Errorf("not a workspace namespace: %q", parts[2])
	}
	if pods := review.Status.User.Extra["authentication.kubernetes.io/pod-name"]; len(pods) != 1 || pods[0] != parts[3]+"-0" {
		return nil, fmt.Errorf("token is not bound to the workspace pod")
	}
	ws, err := a.Store.WorkspaceByID(ctx, parts[3])
	if err != nil {
		return nil, fmt.Errorf("workspace %s: %w", parts[3], err)
	}
	if ws.Owner != owner || ws.Desired != store.DesiredRunning {
		return nil, fmt.Errorf("workspace %s is not a running workspace of %s", ws.ID, owner)
	}
	return ws, nil
}

func (a *AgentServer) config(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	cfg := api.AgentConfig{ID: ws.ID, Name: ws.Name, RepoURL: ws.RepoURL}
	u, err := a.Store.UserByName(r.Context(), ws.Owner)
	switch {
	case err == nil && u.GitName != "":
		cfg.Git = &api.GitIdentity{Name: u.GitName, Email: u.GitEmail}
	case err != nil && !errors.Is(err, store.ErrNotFound):
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// agentPhases are the phases an agent may report.
var agentPhases = map[string]bool{
	api.PhaseCloning: true, api.PhaseSetup: true, api.PhaseClaude: true, api.PhaseSignIn: true,
	api.PhaseReady: true, api.PhaseError: true,
}

// claudeLink is the only kind of link an agent may report.
var claudeLink = regexp.MustCompile(`^https://claude\.ai/code/session_[A-Za-z0-9]+$`)

func (a *AgentServer) status(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	var st api.AgentStatus
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&st); err != nil {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if st.Image != "" {
		// The project image to swap to; the phase stays as it is.
		if !api.ValidImage(st.Image) {
			writeError(w, http.StatusBadRequest, "invalid image")
			return
		}
		if err := a.Store.SetImage(r.Context(), ws.ID, max(st.Boot, 0), st.Image); err != nil {
			internalError(w, err)
			return
		}
		a.Wake()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !agentPhases[st.Phase] {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if len(st.Message) > 500 {
		st.Message = st.Message[:500]
	}
	if len(st.ClaudeLastExit) > 500 {
		st.ClaudeLastExit = st.ClaudeLastExit[:500]
	}
	rec := store.AgentStatus{
		Boot: max(st.Boot, 0), Phase: st.Phase, Message: st.Message,
		ClaudeRestarts: max(st.ClaudeRestarts, 0), ClaudeLastExit: st.ClaudeLastExit,
	}
	if st.Phase == api.PhaseSignIn {
		if st.SignIn == nil || !validSignIn(st.SignIn) {
			writeError(w, http.StatusBadRequest, "invalid sign-in")
			return
		}
		rec.SignInURL, rec.SignInKey = st.SignIn.URL, st.SignIn.PublicKey
	}
	if st.Link != "" {
		if !claudeLink.MatchString(st.Link) {
			writeError(w, http.StatusBadRequest, "invalid link")
			return
		}
		rec.Link = st.Link
	}
	if err := a.Store.SetStatus(r.Context(), ws.ID, rec); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// messages waits up to longPoll for a message for the agent. Each poll is
// also the agent's heartbeat, recorded at most once a minute.
func (a *AgentServer) messages(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	if time.Since(ws.AgentSeenAt) > seenEvery {
		if err := a.Store.SetAgentSeen(r.Context(), ws.ID); err != nil {
			slog.Warn("record agent heartbeat", "workspace", ws.ID, "err", err)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), longPoll)
	defer cancel()
	select {
	case m := <-a.box(ws.ID):
		writeJSON(w, http.StatusOK, m)
	case <-ctx.Done():
		w.WriteHeader(http.StatusNoContent)
	}
}

// agentSilence is how long the agent of a workspace whose phase it reports
// has gone without polling, or 0 if that is under silentAfter.
func agentSilence(ws *store.Workspace, now time.Time) time.Duration {
	if !agentPhases[ws.Phase] || ws.AgentSeenAt.IsZero() {
		return 0
	}
	if d := now.Sub(ws.AgentSeenAt); d > silentAfter {
		return d
	}
	return 0
}

// validSignIn accepts only Claude's own sign-in pages, because the CLI opens
// the URL on the user's machine, and a 32-byte public key.
func validSignIn(s *api.SignIn) bool {
	u, err := url.Parse(s.URL)
	if err != nil || u.Scheme != "https" || (u.Host != "claude.com" && u.Host != "claude.ai") || len(s.URL) > 2048 {
		return false
	}
	key, err := base64.StdEncoding.DecodeString(s.PublicKey)
	return err == nil && len(key) == 32
}
