package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func tarGz(t *testing.T, files map[string]string, extra ...*tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range extra {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstallClaudeSetupReplacesTheThreeItems(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	for path, body := range map[string]string{
		"CLAUDE.md": "old", "skills/gone/SKILL.md": "old", ".credentials.json": "login",
	} {
		p := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	archive := tarGz(t, map[string]string{"CLAUDE.md": "new", "skills/review/SKILL.md": "review"})
	if err := installClaudeSetup(home, archive); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"CLAUDE.md": "new", "skills/review/SKILL.md": "review", ".credentials.json": "login",
	} {
		if b, err := os.ReadFile(filepath.Join(root, path)); err != nil || string(b) != want {
			t.Errorf("%s: %q, %v; want %q", path, b, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "gone")); !os.IsNotExist(err) {
		t.Errorf("a skill missing from the laptop copy is still there: %v", err)
	}
}

func TestInstallClaudeSetupRefusesOtherEntries(t *testing.T) {
	for name, archive := range map[string][]byte{
		"climbs out":   tarGz(t, map[string]string{"skills/../../.bashrc": "x"}),
		"other file":   tarGz(t, map[string]string{".credentials.json": "x"}),
		"symlink":      tarGz(t, nil, &tar.Header{Typeflag: tar.TypeSymlink, Name: "skills/x", Linkname: "/etc/passwd"}),
		"not gzip tar": []byte("nope"),
	} {
		home := t.TempDir()
		claudeMD := filepath.Join(home, ".claude", "CLAUDE.md")
		if err := os.MkdirAll(filepath.Dir(claudeMD), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(claudeMD, []byte("kept"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := installClaudeSetup(home, archive); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if b, _ := os.ReadFile(claudeMD); string(b) != "kept" {
			t.Errorf("%s: a refused archive changed CLAUDE.md to %q", name, b)
		}
	}
}
