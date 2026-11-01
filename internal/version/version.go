// Package version holds the build version shared by all Noma binaries.
package version

// Version is set at build time with -ldflags "-X github.com/MrDHat/noma/internal/version.Version=...".
var Version = "dev"
