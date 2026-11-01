// Package agent is noma-agent: it runs as PID 1 in a workspace pod, dials
// noma-server, sets the workspace up and supervises Claude.
package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/MrDHat/noma/internal/api"
)

// Client calls noma-server's agent endpoint.
type Client struct {
	BaseURL   string
	TokenFile string
	HTTP      *http.Client
}

// NewClient returns a client that trusts only the CA in caFile.
func NewClient(baseURL, tokenFile, caFile string) (*Client, error) {
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no certificates in %s", caFile)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13}}
	return &Client{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		TokenFile: tokenFile,
		// Longer than the server's long-poll.
		HTTP: &http.Client{Transport: transport, Timeout: 60 * time.Second},
	}, nil
}

// Config fetches what this workspace should set up.
func (c *Client) Config(ctx context.Context) (*api.AgentConfig, error) {
	var cfg api.AgentConfig
	_, err := c.do(ctx, http.MethodGet, "/agent/v1/workspace", nil, &cfg)
	return &cfg, err
}

// Status reports the workspace phase.
func (c *Client) Status(ctx context.Context, phase, message string) error {
	_, err := c.do(ctx, http.MethodPost, "/agent/v1/status", api.AgentStatus{Phase: phase, Message: message}, nil)
	return err
}

// NextMessage waits for the next server message. It returns nil when the
// long-poll ends without one.
func (c *Client) NextMessage(ctx context.Context) (*api.AgentMessage, error) {
	var m api.AgentMessage
	status, err := c.do(ctx, http.MethodGet, "/agent/v1/messages", nil, &m)
	if err != nil || status == http.StatusNoContent {
		return nil, err
	}
	return &m, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) (int, error) {
	// The kubelet rotates the token, so read it for every request.
	token, err := os.ReadFile(c.TokenFile)
	if err != nil {
		return 0, fmt.Errorf("read token: %w", err)
	}
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, &buf)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 300 {
		var e api.Error
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return resp.StatusCode, fmt.Errorf("%s %s: %s (%d)", method, path, e.Error, resp.StatusCode)
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		return resp.StatusCode, json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode, nil
}
