//go:build !linux

package preflight

// Sandboxed reports whether this process runs in gVisor, which runs
// Linux only.
func Sandboxed() (bool, error) { return false, nil }
