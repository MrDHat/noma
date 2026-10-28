package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const fixture = `agent: claude
environment:
  image: node:22
  setup:
    - npm install
compute:
  provider: aws
  region: eu-west-1
  instanceType: t3.large
repos:
  - acme/default
profiles:
  fullstack:
    repos:
      - acme/web
      - acme/api
  web.apps:
    repos:
      - acme/dotted
  team=blue:
    repos:
      - acme/equal
`

func main() {
	if err := verify(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("CLI verification passed. JSON output, profiles, typed overrides, rejection paths, and source immutability checked.")
}

func verify() error {
	dir, err := os.MkdirTemp("", "noma-cli-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	binary := filepath.Join(dir, "noma")
	build := exec.Command("go", "build", "-o", binary, "./cmd/noma")
	if output, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build CLI: %w\n%s", err, output)
	}
	path := filepath.Join(dir, "noma.yaml")
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		return err
	}

	run := func(args ...string) (string, string, error) {
		command := exec.Command(binary, args...)
		command.Dir = dir
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		return stdout.String(), stderr.String(), err
	}

	accepted := []struct {
		name string
		args []string
		want map[string]any
	}{
		{"default file", []string{"config"}, map[string]any{"agent": "claude", "repos": []any{"acme/default"}}},
		{"profile", []string{"config", "--file", path, "--profile", "fullstack"}, map[string]any{"repos": []any{"acme/web", "acme/api"}}},
		{"ordered overrides", []string{"config", "--set", "agent=opencode", "--set", "agent=codex"}, map[string]any{"agent": "codex"}},
		{"array override", []string{"config", "--set", `environment.setup=["go version","go test ./..."]`}, map[string]any{"environment": map[string]any{"image": "node:22", "setup": []any{"go version", "go test ./..."}}}},
		{"object override", []string{"config", "--set", `environment={"image":"alpine:3","setup":["echo ok"]}`}, map[string]any{"environment": map[string]any{"image": "alpine:3", "setup": []any{"echo ok"}}}},
		{"array index", []string{"config", "--profile", "fullstack", "--set", "profiles.fullstack.repos[1]=acme/service"}, map[string]any{"repos": []any{"acme/web", "acme/service"}}},
		{"quoted map key", []string{"config", "--profile", "web.apps", "--set", `profiles["web.apps"].repos[0]=acme/quoted`}, map[string]any{"repos": []any{"acme/quoted"}}},
		{"equals in map key", []string{"config", "--profile", "team=blue", "--set", `profiles["team=blue"].repos[0]=acme/changed`}, map[string]any{"repos": []any{"acme/changed"}}},
		{"equals in value", []string{"config", "--set", `environment.setup[0]=echo key=value`}, map[string]any{"environment": map[string]any{"image": "node:22", "setup": []any{"echo key=value"}}}},
		{"quoted numeric string", []string{"config", "--set", `environment.image="123"`}, map[string]any{"environment": map[string]any{"image": "123", "setup": []any{"npm install"}}}},
		{"JSON Unicode escapes", []string{"config", "--set", `environment.image="\ud83d\ude00"`}, map[string]any{"environment": map[string]any{"image": "\U0001F600", "setup": []any{"npm install"}}}},
	}
	for _, tc := range accepted {
		stdout, stderr, err := run(tc.args...)
		if err != nil || stderr != "" {
			return fmt.Errorf("%s: expected success, got %v and stderr %q", tc.name, err, stderr)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			return fmt.Errorf("%s: output is not JSON: %w", tc.name, err)
		}
		for key, want := range tc.want {
			actual, _ := json.Marshal(got[key])
			expected, _ := json.Marshal(want)
			if !bytes.Equal(actual, expected) {
				return fmt.Errorf("%s: %s is %s, want %s", tc.name, key, actual, expected)
			}
		}
		if err := unchanged(path); err != nil {
			return err
		}
	}

	rejected := [][]string{
		{"up", "demo"},
		{"--help", "unexpected"},
		{"--help", "--unknown"},
		{"config", "unexpected"},
		{"config", "--unknown"},
		{"config", "--set"},
		{"config", "--file", filepath.Join(dir, "missing.yaml")},
		{"config", "--profile", "missing"},
		{"config", "--set", "agent=unknown"},
		{"config", "--set", "environment.image=123"},
		{"config", "--set", "environment.image=true"},
		{"config", "--set", "compute.region=null"},
		{"config", "--set", "environment.Image=alpine"},
		{"config", "--set", "compute.instanceTypo=t3.small"},
		{"config", "--set", "profiles.fullstack.agent=codex"},
		{"config", "--set", "environment.setup[4]=echo bad"},
		{"config", "--set", "environment..image=alpine"},
		{"config", "--set", `environment.setup=["broken"`},
		{"config", "--set", `environment={"image":"alpine","image":"ubuntu"}`},
		{"config", "--set", `profiles={"fullstack":{"repos":["acme/a"],"repos":["acme/b"]}}`},
	}
	for _, args := range rejected {
		stdout, stderr, err := run(args...)
		if err == nil || stdout != "" || stderr == "" {
			return fmt.Errorf("%v: expected nonzero exit, empty stdout, and a diagnostic; got %v, %q, %q", args, err, stdout, stderr)
		}
		if err := unchanged(path); err != nil {
			return err
		}
	}

	if err := os.Remove(path); err != nil {
		return err
	}
	for _, args := range [][]string{{"--help"}, {"help"}, {"config", "--help"}} {
		stdout, stderr, err := run(args...)
		if err != nil || stdout+stderr == "" {
			return fmt.Errorf("%v: help requires no config file, got %v", args, err)
		}
	}
	stdout, stderr, err := run("config")
	if err == nil || stdout != "" || stderr == "" {
		return fmt.Errorf("missing default file: expected a diagnostic and nonzero exit")
	}
	return nil
}

func unchanged(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(data) != fixture {
		return fmt.Errorf("CLI modified source configuration %s", path)
	}
	return nil
}
