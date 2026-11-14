package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadNomaYAML(t *testing.T) {
	for _, tc := range []struct {
		name, file, want string
		bad              bool
	}{
		{"no file", "", "", false},
		{"image", "image: ghcr.io/acme/dev:1.2@sha256:" + sha, "ghcr.io/acme/dev:1.2@sha256:" + sha, false},
		{"empty", "# nothing yet\n", "", false},
		{"unknown field", "image: debian\nimages: [x]\n", "", true},
		{"not an image", "image: 'debian; rm -rf /'\n", "", true},
		{"not yaml", "image: [\n", "", true},
		{"setup", "setup: [make tools, 'go mod download']\n", "", false},
		{"empty setup command", "setup: ['  ']\n", "", true},
	} {
		dir := t.TempDir()
		if tc.file != "" {
			if err := os.WriteFile(filepath.Join(dir, "noma.yaml"), []byte(tc.file), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		y, err := readNomaYAML(dir)
		if got := y.Image; (err != nil) != tc.bad || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q, bad=%v", tc.name, got, err, tc.want, tc.bad)
		}
	}
}

const sha = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestRepoNomaYAML(t *testing.T) {
	y, err := readNomaYAML("../..")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(y.Image, "ghcr.io/mrdhat/noma-dev:") {
		t.Errorf("the repo's noma.yaml names %q, want the noma-dev image", y.Image)
	}
	if len(y.Setup) == 0 {
		t.Error("the repo's noma.yaml has no setup commands")
	}
}
