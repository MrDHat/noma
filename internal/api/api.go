// Package api holds the HTTP request and response types shared by the
// noma CLI, noma-server and noma-agent.
package api

import (
	"regexp"
	"time"
)

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
	// AgentSilentSeconds is set when the workspace's agent should be
	// polling noma-server but hasn't for over two minutes.
	AgentSilentSeconds int `json:"agentSilentSeconds,omitempty"`
	// ClaudeRestarts and ClaudeLastExit are as the agent last reported.
	ClaudeRestarts int    `json:"claudeRestarts,omitempty"`
	ClaudeLastExit string `json:"claudeLastExit,omitempty"`
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

// WorkspaceList is the response to listing workspaces.
type WorkspaceList struct {
	Workspaces []Workspace `json:"workspaces"`
}

// CreateWorkspaceRequest is the body of POST /v1/workspaces.
type CreateWorkspaceRequest struct {
	RepoURL string `json:"repoUrl"`
	// Name is optional; the server derives one from the repo URL.
	Name string `json:"name,omitempty"`
	// Git is the git identity on the user's laptop, if set there. It
	// becomes the identity of every workspace the user starts.
	Git *GitIdentity `json:"git,omitempty"`
}

// GitIdentity is a git author: user.name and user.email.
type GitIdentity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// SecretRequest is the body of PUT /v1/secrets/{name}: a value and how to
// deliver it. The value is write-only; no response ever contains it.
type SecretRequest struct {
	Value string `json:"value"`
	// GitHost delivers it as the git credential for this host; Env as this
	// environment variable. At least one is required.
	GitHost string `json:"gitHost,omitempty"`
	Env     string `json:"env,omitempty"`
	// Repos limits it to repos matching these patterns, like
	// github.com/acme/*; empty means all of the user's workspaces.
	Repos []string `json:"repos,omitempty"`
}

// Secret describes a stored secret, without its value.
type Secret struct {
	Name      string    `json:"name"`
	GitHost   string    `json:"gitHost,omitempty"`
	Env       string    `json:"env,omitempty"`
	Repos     []string  `json:"repos,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SecretList is the response to listing secrets.
type SecretList struct {
	Secrets []Secret `json:"secrets"`
}

// Credential is a secret delivered to a workspace, as GET
// /agent/v1/credentials returns it to that workspace's agent.
type Credential struct {
	Name    string `json:"name"`
	GitHost string `json:"gitHost,omitempty"`
	Env     string `json:"env,omitempty"`
	Value   string `json:"value"`
}

// CredentialList is the response to GET /agent/v1/credentials.
type CredentialList struct {
	Credentials []Credential `json:"credentials"`
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
	// Git is the owner's git identity, if known.
	Git *GitIdentity `json:"git,omitempty"`
}

// AgentStatus is the body of POST /agent/v1/status.
type AgentStatus struct {
	Phase   string `json:"phase"`
	Message string `json:"message,omitempty"`
	// Link is the Remote Control link, sent with the ready phase.
	Link string `json:"link,omitempty"`
	// SignIn is sent with the sign-in phase.
	SignIn *SignIn `json:"signIn,omitempty"`
	// Image, when set, is the only thing in the report: the image the
	// repo's noma.yaml names, which this pod is not running yet (M3-17).
	Image string `json:"image,omitempty"`
	// ClaudeRestarts counts how often Claude has restarted in this pod, and
	// ClaudeLastExit says how it last stopped.
	ClaudeRestarts int    `json:"claudeRestarts,omitempty"`
	ClaudeLastExit string `json:"claudeLastExit,omitempty"`
}

// AgentAccount is the body of POST /agent/v1/account: the Claude account
// the workspace's login belongs to. The server answers 409 if it is not the
// account pinned for the user.
type AgentAccount struct {
	Account string `json:"account"`
}

// AgentMessage is one message from the server to the agent, returned by the
// long-poll GET /agent/v1/messages.
type AgentMessage struct {
	Type string `json:"type"`
	// SignInCode is set for MessageSignInCode.
	SignInCode *SignInCode `json:"signInCode,omitempty"`
}

// Message types.
const (
	// MessageSignInCode carries a sealed Claude sign-in code to the agent.
	MessageSignInCode = "signin-code"
	// MessageCredentials says the workspace's credentials changed; the
	// agent fetches them again.
	MessageCredentials = "credentials-changed"
)

// imageRef is a container image reference: an optional registry host, a
// path, and a tag and/or digest.
var imageRef = regexp.MustCompile(`^([a-z0-9-]+(\.[a-z0-9-]+)*(:[0-9]{1,5})?/)?[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*(:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?(@sha256:[a-f0-9]{64})?$`)

// ValidImage reports whether s is a container image reference.
func ValidImage(s string) bool { return len(s) <= 255 && imageRef.MatchString(s) }
