package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/MrDHat/noma/internal/api"
)

// claudeSetupItems are what of ~/.claude a workspace gets (M3.5-5): the
// user's instructions, skills and settings. Nothing else, so no login,
// history or project state leaves the laptop.
var claudeSetupItems = []string{"CLAUDE.md", "settings.json", "skills"}

// packClaudeSetup packs the user's Claude setup from home/.claude as a
// tar.gz. It skips .git directories and symlinks that leave ~/.claude, and
// warns about files that mention this machine's home directory, which
// workspaces don't have.
func packClaudeSetup(home string) (archive []byte, warnings []string, err error) {
	root := filepath.Join(home, ".claude")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(path string, d fs.DirEntry) error {
		rel, _ := filepath.Rel(root, path)
		name := filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: name + "/", Mode: 0o755})
		}
		info, err := os.Stat(path) // follows a symlink
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skipped %s: %v", name, err))
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(path)
			if err != nil || !within(root, target) || info.IsDir() {
				warnings = append(warnings, fmt.Sprintf("skipped %s: a link that leaves ~/.claude or to a directory", name))
				return nil
			}
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(b, []byte(home+"/")) {
			warnings = append(warnings, fmt.Sprintf("%s mentions %s, which workspaces don't have", name, home))
		}
		mode := int64(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: mode, Size: int64(len(b))}); err != nil {
			return err
		}
		_, err = tw.Write(b)
		return err
	}
	for _, item := range claudeSetupItems {
		path := filepath.Join(root, item)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return add(p, d)
		}); err != nil {
			return nil, warnings, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, warnings, err
	}
	if err := gz.Close(); err != nil {
		return nil, warnings, err
	}
	if buf.Len() > api.MaxClaudeSetup {
		return nil, warnings, fmt.Errorf("your Claude setup is %d KiB packed; at most %d KiB", buf.Len()>>10, api.MaxClaudeSetup>>10)
	}
	return buf.Bytes(), warnings, nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// sendClaudeSetup uploads the user's Claude setup, which workspaces copy
// into their ~/.claude when they start. A problem packing it is only a
// warning: workspaces keep the copy from last time.
func sendClaudeSetup(ctx context.Context, c *Client, stdout io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	archive, warnings, err := packClaudeSetup(home)
	for _, w := range warnings {
		fmt.Fprintln(stdout, "warning: Claude setup:", w)
	}
	if err != nil {
		fmt.Fprintf(stdout, "warning: not copying your Claude setup: %v; workspaces keep the last copy\n", err)
		return nil
	}
	return c.PutClaudeSetup(ctx, archive)
}
