package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/MrDHat/noma/internal/api"
)

// ErrNotFound is returned for a 404 from the server.
var ErrNotFound = errors.New("not found")

// Client calls noma-server.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client

	close func()
}

// Connect returns a client for the configured server, starting a port-forward
// if the config asks for one. Call Close when done.
func Connect(ctx context.Context, cfg *Config) (*Client, error) {
	c := &Client{BaseURL: cfg.Server, Token: cfg.Token, HTTP: http.DefaultClient, close: func() {}}
	if cfg.PortForward != nil {
		var err error
		if c.BaseURL, c.close, err = startPortForward(ctx, cfg.PortForward); err != nil {
			return nil, err
		}
	}
	if c.BaseURL == "" {
		return nil, errors.New("config has no server")
	}
	return c, nil
}

// Close stops the port-forward, if any.
func (c *Client) Close() { c.close() }

// CreateWorkspace creates a workspace.
func (c *Client) CreateWorkspace(ctx context.Context, req api.CreateWorkspaceRequest) (*api.Workspace, error) {
	var ws api.Workspace
	return &ws, c.do(ctx, http.MethodPost, "/v1/workspaces", req, &ws)
}

// Workspace returns the workspace called name.
func (c *Client) Workspace(ctx context.Context, name string) (*api.Workspace, error) {
	var ws api.Workspace
	return &ws, c.do(ctx, http.MethodGet, "/v1/workspaces/"+url.PathEscape(name), nil, &ws)
}

// DeleteWorkspace deletes the workspace called name.
func (c *Client) DeleteWorkspace(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/workspaces/"+url.PathEscape(name), nil, nil)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 300 {
		var e api.Error
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %s", ErrNotFound, e.Error)
		}
		return fmt.Errorf("server: %s (%d)", e.Error, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
