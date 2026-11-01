// Package api holds the HTTP request and response types shared by the
// noma CLI, noma-server and noma-agent.
package api

import "time"

// Workspace phases, as reported to the CLI.
const (
	PhasePending  = "pending"
	PhaseStarting = "starting"
	PhaseError    = "error"
	PhaseDeleting = "deleting"
)

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
