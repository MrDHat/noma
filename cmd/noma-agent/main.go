// Command noma-agent runs inside each workspace pod as PID 1.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/MrDHat/noma/internal/agent"
	"github.com/MrDHat/noma/internal/isolation"
	"github.com/MrDHat/noma/internal/version"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println("noma-agent", version.Version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "credential" {
		if err := agent.CredentialHelper(os.Args[2:], os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "isolation-check" {
		if err := isolationCheck(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if code, handled := agent.RunAsInit(); handled {
		os.Exit(code)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("noma-agent failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	c, err := agent.NewClient(
		os.Getenv("NOMA_SERVER_URL"),
		os.Getenv("NOMA_TOKEN_FILE"),
		envOr("NOMA_CA_FILE", "/etc/noma/ca.crt"),
	)
	if err != nil {
		return err
	}
	slog.Info("noma-agent started", "version", version.Version)
	return (&agent.Agent{
		Client:     c,
		Dir:        os.Getenv("NOMA_WORKSPACE_DIR"),
		ClaudeBin:  envOr("NOMA_CLAUDE_BIN", "claude"),
		ProfileURL: envOr("NOMA_CLAUDE_PROFILE_URL", agent.DefaultProfileURL),
	}).Run(ctx)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// isolationCheck checks, from inside a workspace, what it can and can't
// reach (M3.5-10, M3.5-11). The e2e tests and make demo-aws run it.
func isolationCheck(args []string) error {
	fs := flag.NewFlagSet("isolation-check", flag.ContinueOnError)
	var o isolation.Options
	fs.StringVar(&o.Kubelet, "kubelet", "", "the node's address, which must refuse the kubelet port")
	fs.StringVar(&o.Pod, "pod", "", "another pod's host:port, which must be refused")
	fs.StringVar(&o.PublicAPI, "public-api", "", "the cluster's public API endpoint, which must answer 401 or 403")
	fs.StringVar(&o.Internet, "internet", "https://github.com", "a public URL that must answer")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return isolation.Run(context.Background(), o, os.Stdout)
}
