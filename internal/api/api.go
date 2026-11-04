// Package api holds the HTTP request and response types shared by the
// noma CLI, noma-server and noma-agent.
package api

import "time"

// Workspace phases, as reported to the CLI.
const (
	PhasePending  = "pending"
	PhaseStarting = "starting"
	PhaseCloning  = "cloning"
	PhaseClaude   = "starting-claude"
	PhaseSignIn   = "sign-in"
	PhaseReady    = "ready"
	PhaseError    = "error"
	PhaseDeleting = "deleting"
	PhaseStopping = "stopping"
	PhaseStopped  = "stopped"
)

// TokenAudience is the audience of the Kubernetes token a workspace agent
// presents. Tokens for it are useless against the Kubernetes API.
const TokenAudience = "noma"

// Workspace is a workspace as the CLI sees it.
type Workspace struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	RepoURL string `json:"repoUrl"`
	Phase   string `json:"phase"`
	Message string `json:"message,omitempty"`
	// Link opens the workspace's Claude session in claude.ai/code or the app.
	Link string `json:"link,omitempty"`
	// SignIn is set while Claude in the workspace waits for a sign-in code.
	SignIn    *SignIn   `json:"signIn,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// SignIn is a Claude sign-in that needs the user. The CLI opens URL, asks
// for the code Claude shows after approval, and seals it to PublicKey (a
// one-time NaCl box key, base64) so only the workspace can read it.
type SignIn struct {
	URL       string `json:"url"`
	PublicKey string `json:"publicKey"`
}

// SignInCode is the body of POST /v1/workspaces/{name}/signin: the code,
// sealed with box.SealAnonymous to the sign-in's public key, base64.
type SignInCode struct {
	PublicKey  string `json:"publicKey"`
	SealedCode string `json:"sealedCode"`
}

// CreateWorkspaceRequest is the body of POST /v1/workspaces.
type CreateWorkspaceRequest struct {
	RepoURL string `json:"repoUrl"`
	// Name is optional; the server derives one from the repo URL.
	Name string `json:"name,omitempty"`
}

// Error is the body of every non-2xx response.
type Error struct {
	Error string `json:"error"`
}

// AgentConfig is what GET /agent/v1/workspace returns: what the agent should
// set up. The server derives the workspace from the agent's token.
type AgentConfig struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	RepoURL string `json:"repoUrl"`
}

// AgentStatus is the body of POST /agent/v1/status.
type AgentStatus struct {
	Phase   string `json:"phase"`
	Message string `json:"message,omitempty"`
	// Link is the Remote Control link, sent with the ready phase.
	Link string `json:"link,omitempty"`
	// SignIn is sent with the sign-in phase.
	SignIn *SignIn `json:"signIn,omitempty"`
}

// AgentMessage is one message from the server to the agent, returned by the
// long-poll GET /agent/v1/messages.
type AgentMessage struct {
	Type string `json:"type"`
	// SignInCode is set for MessageSignInCode.
	SignInCode *SignInCode `json:"signInCode,omitempty"`
}

// MessageSignInCode carries a sealed Claude sign-in code to the agent.
const MessageSignInCode = "signin-code"
