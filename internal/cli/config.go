// Package cli implements the noma command.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is ~/.config/noma/config.json, written by make dev-up.
type Config struct {
	// Server is the base URL of noma-server, used when PortForward is unset.
	Server string `json:"server,omitempty"`
	Token  string `json:"token"`
	// PortForward reaches a server that has no external endpoint yet.
	PortForward *PortForward `json:"portForward,omitempty"`
}

// PortForward names the Kubernetes Service the CLI forwards to.
type PortForward struct {
	Context   string `json:"context"`
	Namespace string `json:"namespace"`
	Service   string `json:"service"`
	Port      int    `json:"port"`
}

// ConfigPath returns $NOMA_CONFIG, or ~/.config/noma/config.json.
func ConfigPath() (string, error) {
	if p := os.Getenv("NOMA_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "noma", "config.json"), nil
}

// LoadConfig reads the CLI config.
func LoadConfig() (*Config, error) {
	p, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read %s (run make dev-up first): %w", p, err)
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return &c, nil
}
