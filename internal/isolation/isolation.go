// Package isolation checks, from inside a workspace, that the workspace
// can reach the internet but nothing in the cluster it shouldn't
// (M3.5-10, M3.5-11): the Kubernetes API, the kubelet, the cloud's
// metadata service, Postgres and other pods.
package isolation

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

// Options name the targets that depend on the cluster.
type Options struct {
	// Kubelet is the node's address (the pod's hostIP).
	Kubelet string
	// Pod is another pod's host:port, such as noma-server's.
	Pod string
	// PublicAPI is the cluster's public API endpoint (EKS), which must
	// refuse an anonymous request.
	PublicAPI string
	// Internet is a public URL that must answer.
	Internet string
}

// Run checks every target, writes one line per check to w, and returns an
// error if any check failed.
func Run(ctx context.Context, o Options, w io.Writer) error {
	refused := map[string]string{
		"Kubernetes API (service)": "kubernetes.default.svc:443",
		"cloud metadata":           "169.254.169.254:80",
		"Postgres":                 "postgres.noma-system.svc:5432",
	}
	if h, p := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT"); h != "" && p != "" {
		refused["Kubernetes API (address)"] = net.JoinHostPort(h, p)
	}
	if o.Kubelet != "" {
		refused["kubelet"] = net.JoinHostPort(o.Kubelet, "10250")
	}
	if o.Pod != "" {
		refused["another pod"] = o.Pod
	}
	failed := 0
	for name, addr := range refused {
		d := net.Dialer{Timeout: 3 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			failed++
			fmt.Fprintf(w, "FAIL  %s (%s) is reachable\n", name, addr)
			continue
		}
		fmt.Fprintf(w, "ok    %s (%s) is refused\n", name, addr)
	}
	if o.Internet != "" {
		if code, err := status(ctx, o.Internet, false); err != nil || code >= 500 {
			failed++
			fmt.Fprintf(w, "FAIL  the internet (%s) is unreachable: %d %v\n", o.Internet, code, err)
		} else {
			fmt.Fprintf(w, "ok    the internet (%s) answers\n", o.Internet)
		}
	}
	if o.PublicAPI != "" {
		code, err := status(ctx, o.PublicAPI+"/api", true)
		if err != nil || (code != http.StatusUnauthorized && code != http.StatusForbidden) {
			failed++
			fmt.Fprintf(w, "FAIL  the public API endpoint answered %d %v, want 401 or 403\n", code, err)
		} else {
			fmt.Fprintf(w, "ok    the public API endpoint refuses anonymous requests (%d)\n", code)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d isolation checks failed", failed)
	}
	return nil
}

// status returns the HTTP status of a GET to url. insecure skips the
// certificate check: the workspace doesn't have the cluster's CA, and the
// check only reads the status, sending no credentials.
func status(ctx context.Context, url string, insecure bool) (int, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	if insecure {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // reads a status only
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}
