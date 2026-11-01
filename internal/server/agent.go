package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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

// AgentServer serves the in-cluster endpoint that workspace agents dial.
type AgentServer struct {
	Store *store.Store
	Kube  kubernetes.Interface

	mu    sync.Mutex
	boxes map[string]chan api.AgentMessage
}

// Handler returns the HTTP handler for agents.
func (a *AgentServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /agent/v1/workspace", a.authed(a.config))
	mux.Handle("POST /agent/v1/status", a.authed(a.status))
	mux.Handle("GET /agent/v1/messages", a.authed(a.messages))
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

func (a *AgentServer) config(w http.ResponseWriter, _ *http.Request, ws *store.Workspace) {
	writeJSON(w, http.StatusOK, api.AgentConfig{ID: ws.ID, Name: ws.Name, RepoURL: ws.RepoURL})
}

// agentPhases are the phases an agent may report.
var agentPhases = map[string]bool{api.PhaseCloning: true, api.PhaseReady: true, api.PhaseError: true}

func (a *AgentServer) status(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	var st api.AgentStatus
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&st); err != nil || !agentPhases[st.Phase] {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if len(st.Message) > 500 {
		st.Message = st.Message[:500]
	}
	if err := a.Store.SetPhase(r.Context(), ws.ID, st.Phase, st.Message); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// messages waits up to longPoll for a message for the agent.
func (a *AgentServer) messages(w http.ResponseWriter, r *http.Request, ws *store.Workspace) {
	ctx, cancel := context.WithTimeout(r.Context(), longPoll)
	defer cancel()
	select {
	case m := <-a.box(ws.ID):
		writeJSON(w, http.StatusOK, m)
	case <-ctx.Done():
		w.WriteHeader(http.StatusNoContent)
	}
}
