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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/MrDHat/noma/internal/preflight"
	"github.com/MrDHat/noma/internal/reconcile"
	"github.com/MrDHat/noma/internal/secrets"
	"github.com/MrDHat/noma/internal/server"
	"github.com/MrDHat/noma/internal/store"
	"github.com/MrDHat/noma/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version":
			fmt.Println("noma-server", version.Version)
			return
		case "preflight":
			if err := runPreflight(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println("the cluster has everything Noma needs")
			return
		case "sandboxed":
			ok, err := preflight.Sandboxed()
			if err != nil || !ok {
				fmt.Fprintln(os.Stderr, "not running in gVisor", err)
				os.Exit(1)
			}
			return
		}
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
	secretKey, err := secrets.ParseKey(os.Getenv("NOMA_SECRET_KEY_ID"), os.Getenv("NOMA_SECRET_KEY"))
	if err != nil {
		return fmt.Errorf("NOMA_SECRET_KEY: %w", err)
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
	sizes, err := workspaceSizes()
	if err != nil {
		return err
	}
	tlsDir := envOr("NOMA_TLS_DIR", "/etc/noma/tls")
	agentCA, err := os.ReadFile(tlsDir + "/ca.crt")
	if err != nil {
		return fmt.Errorf("agent CA: %w", err)
	}
	// With the fake Claude (CI), noma-server stands in for Claude's profile
	// endpoint so the account check has something to ask.
	fakeClaude := os.Getenv("NOMA_FAKE_CLAUDE") == "1"
	agentURL := envOr("NOMA_AGENT_URL", "https://noma-server.noma-system.svc:8443")
	profileURL := ""
	if fakeClaude {
		profileURL = agentURL + "/fake/claude/profile"
	}
	cfg := sizes
	cfg.AgentCA = string(agentCA)
	cfg.Image = envOr("NOMA_WORKSPACE_IMAGE", "noma-workspace:dev")
	cfg.ServerURL = agentURL
	cfg.ClaudeProfileURL = profileURL
	rec := reconcile.New(st, kube, cfg)
	go rec.Run(ctx)

	// The CLI API, and the agent endpoint, which only workspaces reach and
	// which always uses TLS because agents send their token on every request.
	agents := &server.AgentServer{Store: st, Kube: kube, Wake: rec.Wake, Credentials: server.StoredCredentials{Store: st, Key: secretKey}, FakeClaude: fakeClaude}
	cliAPI := &http.Server{
		Addr:              envOr("NOMA_LISTEN", ":8080"),
		Handler:           (&server.Server{Store: st, AdminToken: token, Wake: rec.Wake, SendToAgent: agents.Send, SecretKey: secretKey}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	agentAPI := &http.Server{
		Addr:              envOr("NOMA_AGENT_LISTEN", ":8443"),
		Handler:           agents.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
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

// workspaceSizes reads the workspace sizing and storage settings, which
// the chart sets from its values (M3.5-7, M3.5-20, M3.5-23).
func workspaceSizes() (reconcile.Config, error) {
	var cfg reconcile.Config
	q := map[string]resource.Quantity{}
	for _, v := range []struct{ env, def string }{
		{"NOMA_WORKSPACE_MEMORY_REQUEST", "512Mi"},
		{"NOMA_WORKSPACE_MEMORY_LIMIT", "3Gi"},
		{"NOMA_WORKSPACE_CPU_REQUEST", "100m"},
		{"NOMA_WORKSPACE_CPU_LIMIT", "2"},
		{"NOMA_WORKSPACE_DISK", "20Gi"},
	} {
		parsed, err := resource.ParseQuantity(envOr(v.env, v.def))
		if err != nil {
			return cfg, fmt.Errorf("%s: %w", v.env, err)
		}
		q[v.env] = parsed
	}
	cfg.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: q["NOMA_WORKSPACE_MEMORY_REQUEST"],
			corev1.ResourceCPU:    q["NOMA_WORKSPACE_CPU_REQUEST"],
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: q["NOMA_WORKSPACE_MEMORY_LIMIT"],
			corev1.ResourceCPU:    q["NOMA_WORKSPACE_CPU_LIMIT"],
		},
	}
	cfg.DiskSize = q["NOMA_WORKSPACE_DISK"]
	cfg.RuntimeClass = envOr("NOMA_RUNTIME_CLASS", "gvisor")
	cfg.StorageClass = os.Getenv("NOMA_WORKSPACE_STORAGE_CLASS")
	cfg.LoginStorageClass = os.Getenv("NOMA_LOGIN_STORAGE_CLASS")
	cfg.LoginAccessMode = corev1.PersistentVolumeAccessMode(envOr("NOMA_LOGIN_ACCESS_MODE", string(corev1.ReadWriteOnce)))
	switch cfg.LoginAccessMode {
	case corev1.ReadWriteOnce, corev1.ReadWriteMany:
	default:
		return cfg, fmt.Errorf("NOMA_LOGIN_ACCESS_MODE must be ReadWriteOnce or ReadWriteMany, not %q", cfg.LoginAccessMode)
	}
	return cfg, nil
}

// runPreflight is the chart's pre-install and pre-upgrade hook (M3.5-15):
// it fails, naming what is missing, unless the cluster has what the
// server's settings need.
func runPreflight() error {
	sizes, err := workspaceSizes()
	if err != nil {
		return err
	}
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("kubernetes config: %w", err)
	}
	kube, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return preflight.Check(ctx, kube, preflight.Options{
		RuntimeClass:   sizes.RuntimeClass,
		StorageClasses: []string{sizes.StorageClass, sizes.LoginStorageClass},
		Namespace:      os.Getenv("POD_NAMESPACE"),
		Image:          os.Getenv("NOMA_PREFLIGHT_IMAGE"),
		Timeout:        2 * time.Minute,
	})
}
