// Command fakeclaude stands in for Claude Code in CI, where real sign-in
// isn't possible. It mimics only what noma-agent uses, with the same output
// and files as Claude Code 2.1.316: auth status; auth login, which prints the
// authorize URL, asks for the code and accepts any code starting with
// "fake-code-"; and remote-control, which needs a login, a terminal and a
// trusted directory, records its environment in bridge-pointer.json and
// prints the claude.ai/code link.
package main

import (
	"bufio"
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
	"time"

	"golang.org/x/term"
)

func main() {
	args := strings.Join(os.Args[1:], " ")
	switch {
	case args == "--version":
		fmt.Println("2.1.316 (Claude Code) [fake]")
	case args == "auth status":
		authStatus()
	case args == "auth login":
		authLogin()
	case strings.HasPrefix(args, "remote-control"):
		remoteControl()
	default:
		fmt.Fprintf(os.Stderr, "fakeclaude: unsupported command %q\n", args)
		os.Exit(2)
	}
}

func credentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", ".credentials.json")
}

func loggedIn() bool {
	_, err := os.Stat(credentialsPath())
	return err == nil
}

func authStatus() {
	method := "none"
	if loggedIn() {
		method = "claude.ai"
	}
	out, _ := json.MarshalIndent(map[string]any{
		"loggedIn":    loggedIn(),
		"authMethod":  method,
		"apiProvider": "firstParty",
	}, "", "  ")
	fmt.Println(string(out))
	if !loggedIn() {
		os.Exit(1)
	}
}

// fakeAccount is the account every fake sign-in belongs to.
const fakeAccount = "fake-account"

func authLogin() {
	url := "https://claude.com/cai/oauth/authorize?code=true&client_id=fake&response_type=code" +
		"&code_challenge=fake&code_challenge_method=S256&state=" + randomHex(8)
	fmt.Printf("Opening browser to sign in…\r\nIf the browser didn't open, visit: \x1b]8;;%s\x07%s\x1b]8;;\x07\r\nPaste code here if prompted > ", url, url)
	code, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if !strings.HasPrefix(strings.TrimSpace(code), "fake-code-") {
		fmt.Print("\r\nOAuth error: Invalid code\r\n")
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(credentialsPath()), 0o700); err == nil {
		// Shaped like Claude's: the token names the account, for noma-server's
		// fake profile endpoint.
		creds := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"fake-token-%s","expiresAt":%d}}`,
			fakeAccount, time.Now().Add(8*time.Hour).UnixMilli())
		err = os.WriteFile(credentialsPath(), []byte(creds), 0o600)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	fmt.Print("\r\nLogin successful.\r\n")
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func remoteControl() {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "Error: Remote Control needs an interactive terminal.")
		os.Exit(1)
	}
	if !loggedIn() {
		fmt.Fprint(os.Stderr, "Error: You must be logged in to use Remote Control.\r\n")
		os.Exit(1)
	}
	home, _ := os.UserHomeDir()
	dir, _ := os.Getwd()
	if !trusted(home, dir) {
		fmt.Fprintln(os.Stderr, "Error: Workspace not trusted. Run claude in this directory and accept the trust dialog first.")
		os.Exit(1)
	}
	key := regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(dir, "-")
	project := filepath.Join(home, ".claude", "projects", key)
	// Like Claude, resume the environment recorded for this directory, so
	// the link survives a restart.
	env := resumedEnv(filepath.Join(project, "bridge-pointer.json"))
	if env == "" {
		env = "env_fake" + randomHex(12)
	}
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

func resumedEnv(pointer string) string {
	var p struct {
		EnvironmentID string `json:"environmentId"`
	}
	if b, err := os.ReadFile(pointer); err == nil && json.Unmarshal(b, &p) == nil {
		return p.EnvironmentID
	}
	return ""
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
