// Command noma-server runs the Noma API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/MrDHat/noma/internal/reconcile"
	"github.com/MrDHat/noma/internal/server"
	"github.com/MrDHat/noma/internal/store"
	"github.com/MrDHat/noma/internal/version"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println("noma-server", version.Version)
		return
	}
	if err := run(); err != nil {
		slog.Error("noma-server failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	token := os.Getenv("NOMA_ADMIN_TOKEN")
	if token == "" {
		return errors.New("NOMA_ADMIN_TOKEN is not set")
	}
	st, err := store.Open(ctx, os.Getenv("NOMA_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer st.Close() //nolint:errcheck

	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("kubernetes config: %w", err)
	}
	kube, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}
	rec := reconcile.New(st, kube, reconcile.Config{
		Image:     envOr("NOMA_WORKSPACE_IMAGE", "noma-workspace:dev"),
		ServerURL: envOr("NOMA_AGENT_URL", "https://noma-server.noma-system.svc:8443"),
	})
	go rec.Run(ctx)

	// The CLI API, and the agent endpoint, which only workspaces reach and
	// which always uses TLS because agents send their token on every request.
	cliAPI := &http.Server{
		Addr:              envOr("NOMA_LISTEN", ":8080"),
		Handler:           (&server.Server{Store: st, AdminToken: token, Wake: rec.Wake}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	agentAPI := &http.Server{
		Addr:              envOr("NOMA_AGENT_LISTEN", ":8443"),
		Handler:           (&server.AgentServer{Store: st, Kube: kube}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	tlsDir := envOr("NOMA_TLS_DIR", "/etc/noma/tls")

	errc := make(chan error, 2)
	go func() { errc <- cliAPI.ListenAndServe() }()
	go func() { errc <- agentAPI.ListenAndServeTLS(tlsDir+"/tls.crt", tlsDir+"/tls.key") }()
	slog.Info("noma-server started", "version", version.Version, "api", cliAPI.Addr, "agents", agentAPI.Addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return errors.Join(cliAPI.Shutdown(shutdownCtx), agentAPI.Shutdown(shutdownCtx))
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
