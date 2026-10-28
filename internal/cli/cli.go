package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/MrDHat/noma/internal/config"
)

// Run executes the CLI and returns its process exit status.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "Usage: noma config [--file path] [--profile name] [--set path=value]")
		return 2
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		fmt.Fprintln(stdout, "Usage: noma config [--file path] [--profile name] [--set path=value]")
		fmt.Fprintln(stdout, "Validate noma.yaml and print the effective configuration as JSON.")
		return 0
	}
	if args[0] != "config" {
		fmt.Fprintf(stderr, "unknown command %q; use noma --help\n", args[0])
		return 2
	}
	flags := flag.NewFlagSet("noma config", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("file", "noma.yaml", "configuration file")
	profile := flags.String("profile", "", "repository group to select")
	var overrides []string
	flags.Func("set", "override path=value in memory; may be repeated", func(value string) error {
		overrides = append(overrides, value)
		return nil
	})
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "noma config does not accept positional arguments")
		return 2
	}
	data, err := os.ReadFile(*path)
	if err != nil {
		fmt.Fprintf(stderr, "read configuration: %v\n", err)
		return 1
	}
	project, err := config.Parse(data, config.Options{Profile: *profile, Overrides: overrides})
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", *path, err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(project); err != nil {
		fmt.Fprintf(stderr, "write configuration: %v\n", err)
		return 1
	}
	return 0
}
