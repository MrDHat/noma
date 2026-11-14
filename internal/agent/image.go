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
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/MrDHat/noma/internal/api"
)

// errSwapping means the agent reported the repo's image and waits for the
// pod to be replaced with one running it.
var errSwapping = errors.New("waiting for the project image")

// nomaYAML is the repo's noma.yaml. Any field it doesn't know is an
// error, so a typo doesn't go unnoticed (M3-16).
type nomaYAML struct {
	Image string `json:"image"`
	// Setup are shell commands that prepare the checkout before Claude
	// starts (M3.5-12).
	Setup []string `json:"setup"`
}

// maxSetupCommands bounds noma.yaml's setup list.
const maxSetupCommands = 50

// readNomaYAML returns the repo's noma.yaml, or an empty one if it has
// none.
func readNomaYAML(repo string) (nomaYAML, error) {
	var y nomaYAML
	f, err := os.Open(filepath.Join(repo, "noma.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return y, nil
	}
	if err != nil {
		return y, err
	}
	defer f.Close() //nolint:errcheck
	b, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err != nil {
		return y, err
	}
	if err := yaml.UnmarshalStrict(b, &y); err != nil {
		return nomaYAML{}, fmt.Errorf("noma.yaml: %w", err)
	}
	if y.Image != "" && !api.ValidImage(y.Image) {
		return nomaYAML{}, fmt.Errorf("noma.yaml: %q is not an image reference", y.Image)
	}
	if len(y.Setup) > maxSetupCommands {
		return nomaYAML{}, fmt.Errorf("noma.yaml: setup lists %d commands; at most %d", len(y.Setup), maxSetupCommands)
	}
	for i, c := range y.Setup {
		if strings.TrimSpace(c) == "" {
			return nomaYAML{}, fmt.Errorf("noma.yaml: setup command %d is empty", i+1)
		}
	}
	return y, nil
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

// useImage makes sure the pod runs the image the repo's noma.yaml names,
// and returns the setup commands it lists. On another image it reports the
// one wanted and returns errSwapping: the reconciler replaces the pod.
func (a *Agent) useImage(ctx context.Context, repo string) ([]string, error) {
	y, err := readNomaYAML(repo)
	if err != nil {
		return nil, err
	}
	want := y.Image
	current := os.Getenv("NOMA_PROJECT_IMAGE")
	switch {
	case want != "" && want != current:
		slog.Info("switching to the project image", "image", want)
		a.report(ctx, api.AgentStatus{Image: want})
		return nil, errSwapping
	case want == "" && current != "":
		slog.Warn("noma.yaml no longer names an image; staying on the project image", "image", current)
	}
	return y.Setup, nil
}
