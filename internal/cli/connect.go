package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// connectCmd writes the CLI config for an install (M3.5-17). It reads the
// admin token from the install's Secret with the caller's own kubeconfig,
// so whoever may read that Secret may use Noma, and nobody copies tokens.
func connectCmd(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	kubeContext := fs.String("context", "", "kubeconfig context of the cluster (default: the current context)")
	namespace := fs.String("namespace", "noma-system", "namespace Noma is installed in")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: noma connect [--context CONTEXT] [--namespace NAMESPACE]")
	}
	if *kubeContext == "" {
		out, err := kubectl(ctx, "config", "current-context")
		if err != nil {
			return fmt.Errorf("no --context and no current kubeconfig context: %w", err)
		}
		*kubeContext = out
	}
	enc, err := kubectl(ctx, "--context", *kubeContext, "-n", *namespace,
		"get", "secret", "noma-server", "-o", "jsonpath={.data.admin-token}")
	if err != nil {
		return fmt.Errorf("read the admin token: %w", err)
	}
	token, err := base64.StdEncoding.DecodeString(enc)
	if err != nil || len(token) == 0 {
		return fmt.Errorf("secret %s/noma-server has no admin-token", *namespace)
	}
	cfg := &Config{Token: string(token), PortForward: &PortForward{
		Context: *kubeContext, Namespace: *namespace, Service: "noma-server", Port: 8080,
	}}
	// Check the config works before saving it.
	c, err := Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer c.Close()
	if _, err := c.Workspaces(ctx); err != nil {
		return fmt.Errorf("reach noma-server: %w", err)
	}
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := saveConfig(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "connected to Noma in %s/%s; wrote %s\n", *kubeContext, *namespace, path)
	return nil
}

// saveConfig writes the config readable by its owner only: it holds the
// admin token.
func saveConfig(path string, cfg *Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func kubectl(ctx context.Context, args ...string) (string, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("kubectl %s: %w: %s", args[len(args)-1], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
