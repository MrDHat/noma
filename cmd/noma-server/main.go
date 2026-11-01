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

	srv := &http.Server{
		Addr:              envOr("NOMA_LISTEN", ":8080"),
		Handler:           (&server.Server{Store: st, AdminToken: token}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("noma-server started", "version", version.Version, "addr", srv.Addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
