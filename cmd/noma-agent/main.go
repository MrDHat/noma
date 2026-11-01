// Command noma-agent runs inside each workspace pod as PID 1.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/MrDHat/noma/internal/agent"
	"github.com/MrDHat/noma/internal/version"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println("noma-agent", version.Version)
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
	return (&agent.Agent{Client: c, Dir: os.Getenv("NOMA_WORKSPACE_DIR")}).Run(ctx)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
