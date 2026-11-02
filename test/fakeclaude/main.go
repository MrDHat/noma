// Command fakeclaude stands in for Claude Code in CI, where real sign-in
// isn't possible. It mimics only what noma-agent uses, with the same output
// and files as Claude Code 2.1.316: auth status, and remote-control, which
// needs a terminal and a trusted directory, records its environment in
// bridge-pointer.json and prints the claude.ai/code link.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"golang.org/x/term"
)

func main() {
	args := strings.Join(os.Args[1:], " ")
	switch {
	case args == "--version":
		fmt.Println("2.1.316 (Claude Code) [fake]")
	case args == "auth status":
		authStatus()
	case strings.HasPrefix(args, "remote-control"):
		remoteControl()
	default:
		fmt.Fprintf(os.Stderr, "fakeclaude: unsupported command %q\n", args)
		os.Exit(2)
	}
}

func authStatus() {
	out, _ := json.MarshalIndent(map[string]any{
		"loggedIn":    true,
		"authMethod":  "claude.ai",
		"apiProvider": "firstParty",
	}, "", "  ")
	fmt.Println(string(out))
}

func remoteControl() {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "Error: Remote Control needs an interactive terminal.")
		os.Exit(1)
	}
	home, _ := os.UserHomeDir()
	dir, _ := os.Getwd()
	if !trusted(home, dir) {
		fmt.Fprintln(os.Stderr, "Error: Workspace not trusted. Run claude in this directory and accept the trust dialog first.")
		os.Exit(1)
	}
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	env := "env_fake" + hex.EncodeToString(b)
	key := regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(dir, "-")
	project := filepath.Join(home, ".claude", "projects", key)
	pointer, _ := json.Marshal(map[string]any{"environmentId": env, "pid": os.Getpid()})
	if err := os.MkdirAll(project, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(project, "bridge-pointer.json"), pointer, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("\x1b[32m·\x1b[0m Remote Control active\r\n  https://claude.ai/code?environment=%s\r\n", env)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	<-sigs
}

func trusted(home, dir string) bool {
	b, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return false
	}
	var cfg struct {
		Projects map[string]struct {
			HasTrustDialogAccepted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	return json.Unmarshal(b, &cfg) == nil && cfg.Projects[dir].HasTrustDialogAccepted
}
