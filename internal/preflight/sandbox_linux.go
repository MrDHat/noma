package preflight

import (
	"errors"
	"strings"
	"syscall"
)

// Sandboxed reports whether this process runs in gVisor: gVisor's kernel
// log starts with "Starting gVisor...", which no Linux kernel prints.
func Sandboxed() (bool, error) {
	buf := make([]byte, 64<<10)
	n, err := syscall.Klogctl(3, buf) // SYSLOG_ACTION_READ_ALL
	if err != nil {
		if errors.Is(err, syscall.EPERM) {
			return false, nil // a real kernel that hides its log
		}
		return false, err
	}
	return strings.Contains(string(buf[:n]), "gVisor"), nil
}
