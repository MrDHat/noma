//go:build e2e

package e2e

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/MrDHat/noma/internal/cli"
)

func TestUpClonesTheRepo(t *testing.T) {
	out := mustNoma(t, "up", "--name", "e2e-clone", demoRepo)
	t.Cleanup(func() { _, _ = noma(t, "rm", "e2e-clone") })
	if !strings.Contains(out, "cloning") || !strings.HasSuffix(strings.TrimSpace(out), "ready") {
		t.Fatalf("noma up did not show progress up to ready:\n%s", out)
	}
	ws, err := client(t).Workspace(context.Background(), "e2e-clone")
	if err != nil {
		t.Fatal(err)
	}
	kubectl(t, "-n", userNamespace, "exec", ws.ID+"-0", "--",
		"test", "-f", "/workspaces/"+ws.ID+"/e2e-clone/README")
}

func TestAgentEndpointRefusesBadTokens(t *testing.T) {
	base := agentEndpoint(t)
	other := strings.TrimSpace(kubectl(t, "-n", "noma-system", "create", "token", "default", "--audience", "noma"))
	for name, header := range map[string]string{
		"no token":            "",
		"garbage token":       "Bearer not-a-token",
		"non-workspace token": "Bearer " + other,
	} {
		req, _ := http.NewRequest(http.MethodGet, base+"/agent/v1/workspace", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		resp, err := agentHTTP(t).Do(req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, resp.StatusCode)
		}
	}
}

// agentEndpoint forwards a local port to the server's agent endpoint.
func agentEndpoint(t *testing.T) string {
	t.Helper()
	c, err := cli.Connect(context.Background(), &cli.Config{PortForward: &cli.PortForward{
		Context: kubeContext(), Namespace: "noma-system", Service: "noma-server", Port: 8443,
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return strings.Replace(c.BaseURL, "http://", "https://", 1)
}

// agentHTTP trusts the dev CA, like the workspace image does.
func agentHTTP(t *testing.T) *http.Client {
	t.Helper()
	pem, err := os.ReadFile("../../.dev/certs/ca.crt")
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pem)
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: pool, ServerName: "noma-server.noma-system.svc",
	}}}
}
