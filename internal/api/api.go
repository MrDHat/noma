// Package api holds the HTTP request and response types shared by the
// noma CLI, noma-server and noma-agent.
package api

import "time"

// Workspace phases, as reported to the CLI.
const (
	PhasePending  = "pending"
	PhaseStarting = "starting"
	PhaseCloning  = "cloning"
	PhaseReady    = "ready"
	PhaseError    = "error"
	PhaseDeleting = "deleting"
)

// TokenAudience is the audience of the Kubernetes token a workspace agent
// presents. Tokens for it are useless against the Kubernetes API.
const TokenAudience = "noma"

// Workspace is a workspace as the CLI sees it.
type Workspace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	RepoURL   string    `json:"repoUrl"`
	Phase     string    `json:"phase"`
	Message   string    `json:"message,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
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
}

// AgentMessage is one message from the server to the agent, returned by the
// long-poll GET /agent/v1/messages.
type AgentMessage struct {
	Type string `json:"type"`
}
