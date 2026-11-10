package agent

import (
	"os"
	"path/filepath"
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
	} {
		dir := t.TempDir()
		if tc.file != "" {
			if err := os.WriteFile(filepath.Join(dir, "noma.yaml"), []byte(tc.file), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		got, err := readNomaYAML(dir)
		if (err != nil) != tc.bad || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q, bad=%v", tc.name, got, err, tc.want, tc.bad)
		}
	}
}

const sha = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
