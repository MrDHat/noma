package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"sigs.k8s.io/yaml"

	"github.com/MrDHat/noma/internal/api"
)

// errSwapping means the agent reported the repo's image and waits for the
// pod to be replaced with one running it.
var errSwapping = errors.New("waiting for the project image")

// nomaYAML is the repo's noma.yaml. M3 knows one field; any other is an
// error, so a typo doesn't go unnoticed (M3-16).
type nomaYAML struct {
	Image string `json:"image"`
}

// readNomaYAML returns the image the repo's noma.yaml names, or "" if it
// has none.
func readNomaYAML(repo string) (string, error) {
	f, err := os.Open(filepath.Join(repo, "noma.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck
	b, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err != nil {
		return "", err
	}
	var y nomaYAML
	if err := yaml.UnmarshalStrict(b, &y); err != nil {
		return "", fmt.Errorf("noma.yaml: %w", err)
	}
	if y.Image != "" && !api.ValidImage(y.Image) {
		return "", fmt.Errorf("noma.yaml: %q is not an image reference", y.Image)
	}
	return y.Image, nil
}

// checkImage makes sure a project image can run Noma: git, and the glibc
// Claude needs. It runs before setup, which uses git.
func (a *Agent) checkImage(ctx context.Context) error {
	image := os.Getenv("NOMA_PROJECT_IMAGE")
	if image == "" {
		return nil
	}
	for _, cmd := range [][]string{{"git", "--version"}, {a.ClaudeBin, "--version"}} {
		if out, err := exec.CommandContext(ctx, cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("image %s can't run Noma (it needs glibc and git): %s: %v %s", image, filepath.Base(cmd[0]), err, lastLine(out))
		}
	}
	return nil
}

// useImage makes sure the pod runs the image the repo's noma.yaml names. On
// another image it reports the one wanted and returns errSwapping: the
// reconciler replaces the pod.
func (a *Agent) useImage(ctx context.Context, repo string) error {
	want, err := readNomaYAML(repo)
	if err != nil {
		return err
	}
	current := os.Getenv("NOMA_PROJECT_IMAGE")
	switch {
	case want != "" && want != current:
		slog.Info("switching to the project image", "image", want)
		a.report(ctx, api.AgentStatus{Image: want})
		return errSwapping
	case want == "" && current != "":
		slog.Warn("noma.yaml no longer names an image; staying on the project image", "image", current)
	}
	return nil
}
