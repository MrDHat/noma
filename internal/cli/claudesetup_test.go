package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPackClaudeSetup(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	outside := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "CLAUDE.md"), "Be brief.\n")
	write(filepath.Join(root, "settings.json"), `{"hooks": "`+home+`/bin/hook"}`)
	write(filepath.Join(root, "skills", "review", "SKILL.md"), "review things\n")
	write(filepath.Join(root, "skills", "review", ".git", "HEAD"), "ref\n")
	write(filepath.Join(root, ".credentials.json"), "secret\n")
	write(filepath.Join(root, "history.jsonl"), "private\n")
	write(filepath.Join(outside, "evil.md"), "outside\n")
	if err := os.Symlink(filepath.Join(outside, "evil.md"), filepath.Join(root, "skills", "evil.md")); err != nil {
		t.Fatal(err)
	}

	archive, warnings, err := packClaudeSetup(home)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
	want := []string{"CLAUDE.md", "settings.json", "skills/", "skills/review/", "skills/review/SKILL.md"}
	if !slices.Equal(names, want) {
		t.Errorf("packed %q, want %q", names, want)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "settings.json mentions") || !strings.Contains(joined, "skills/evil.md") {
		t.Errorf("warnings %q should flag the laptop path and the outside link", warnings)
	}
}

func TestPackClaudeSetupWithNoClaudeDir(t *testing.T) {
	archive, warnings, err := packClaudeSetup(t.TempDir())
	if err != nil || len(warnings) != 0 || len(archive) == 0 {
		t.Fatalf("got %d bytes, %q, %v; want an empty archive", len(archive), warnings, err)
	}
}
