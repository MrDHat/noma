package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// bootFile on the workspace volume counts the agent starts on it.
const bootFile = ".noma-boot"

// countBoot adds this start to the count on the volume and returns it.
// Only one agent runs on a volume at a time, and the count is saved
// before the agent reports anything, so a later agent always reports a
// higher boot than an earlier one, even one that crashed mid-report.
func countBoot(dir string) (int64, error) {
	path := filepath.Join(dir, bootFile)
	var n int64
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if n, err = strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err != nil {
			return 0, fmt.Errorf("parse %s: %w", path, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return 0, err
	}
	n++
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	_, err = fmt.Fprintln(f, n)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		return 0, fmt.Errorf("save boot count: %w", err)
	}
	return n, nil
}
