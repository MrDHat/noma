package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MrDHat/noma/internal/cli"
)

func TestHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"-h"}, {"help"}} {
		var stdout, stderr bytes.Buffer
		if status := cli.Run(args, &stdout, &stderr); status != 0 {
			t.Fatalf("%v: status %d, stderr %q", args, status, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Usage: noma") || stderr.Len() != 0 {
			t.Fatalf("%v: stdout %q, stderr %q", args, stdout.String(), stderr.String())
		}
	}
}

func TestUnsupportedCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := cli.Run([]string{"up", "demo"}, &stdout, &stderr); status == 0 {
		t.Fatal("unsupported command succeeded")
	}
	if stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("stdout %q, stderr %q", stdout.String(), stderr.String())
	}
}
