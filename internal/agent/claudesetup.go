package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxClaudeSetupBytes bounds the unpacked Claude setup.
const maxClaudeSetupBytes = 64 << 20

// installClaudeSetup replaces CLAUDE.md, skills and settings.json in
// home/.claude with the user's copy from their laptop (M3.5-5), so Claude
// here follows the same instructions. It unpacks into a scratch directory
// first, so a bad archive leaves the old setup in place.
func installClaudeSetup(home string, archive []byte) error {
	root := filepath.Join(home, ".claude")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(root, ".noma-setup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck
	if len(archive) > 0 {
		if err := unpackClaudeSetup(tmp, archive); err != nil {
			return fmt.Errorf("unpack the Claude setup: %w", err)
		}
	}
	for _, item := range []string{"CLAUDE.md", "settings.json", "skills"} {
		if err := os.RemoveAll(filepath.Join(root, item)); err != nil {
			return err
		}
		err := os.Rename(filepath.Join(tmp, item), filepath.Join(root, item))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// unpackClaudeSetup writes the archive's regular files and directories
// under dir. It takes only the three items the CLI packs, and refuses
// anything else: links, devices, or a path that climbs out.
func unpackClaudeSetup(dir string, archive []byte) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(h.Name)
		top, _, _ := strings.Cut(name, "/")
		if path.IsAbs(name) || strings.HasPrefix(name, "..") ||
			(top != "CLAUDE.md" && top != "settings.json" && top != "skills") {
			return fmt.Errorf("unexpected entry %q", h.Name)
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			total += h.Size
			if total > maxClaudeSetupBytes {
				return errors.New("too large")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			mode := os.FileMode(0o600)
			if h.Mode&0o111 != 0 {
				mode = 0o700
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, err = io.CopyN(f, tr, h.Size)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected entry type for %q", h.Name)
		}
	}
}
