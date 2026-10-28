package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MrDHat/noma/internal/cli"
)

func TestRunPrintsEffectiveConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noma.yaml")
	data := []byte("agent: claude\nenvironment: {image: node:22}\ncompute: {provider: aws}\nprofiles: {fullstack: {repos: [acme/web, acme/api]}}\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	status := cli.Run([]string{"config", "--file", path, "--profile", "fullstack", "--set", "agent=codex"}, &stdout, &stderr)
	if status != 0 || stderr.Len() != 0 {
		t.Fatalf("exit %d, stderr %s", status, &stderr)
	}
	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["agent"] != "codex" || !reflect.DeepEqual(got["repos"], []any{"acme/web", "acme/api"}) {
		t.Fatalf("unexpected output %s", &stdout)
	}
	if _, exists := got["profiles"]; exists {
		t.Fatal("output includes unselected profile definitions")
	}
	remaining, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(remaining, data) {
		t.Fatal("CLI changed source configuration")
	}
}

func TestRunHelpNeedsNoFile(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"config", "--help"}, {"config", "--file", "/missing/noma.yaml", "--help"}} {
		var stdout, stderr bytes.Buffer
		if status := cli.Run(args, &stdout, &stderr); status != 0 {
			t.Fatalf("%v exited %d: %s", args, status, &stderr)
		}
		if stdout.Len()+stderr.Len() == 0 {
			t.Fatalf("%v printed no help", args)
		}
	}
}

func TestRunRejectsInvalidArgumentsAndFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noma.yaml")
	if err := os.WriteFile(path, []byte("agent: invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		nil, {"unknown"}, {"--help", "unexpected"}, {"--help", "--unknown"}, {"-h", "unexpected"}, {"help", "unexpected"}, {"config", "--unknown"}, {"config", "--file"}, {"config", "--set"}, {"config", "extra"}, {"config", "--file", path, "extra"}, {"config", "--file", "/missing/noma.yaml"}, {"config", "--file", path},
	} {
		var stdout, stderr bytes.Buffer
		if status := cli.Run(args, &stdout, &stderr); status == 0 {
			t.Fatalf("%v accepted", args)
		}
		if stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("%v produced stdout %q and stderr %q", args, &stdout, &stderr)
		}
	}
}
