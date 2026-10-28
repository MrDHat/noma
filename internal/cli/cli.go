package cli

import (
	"fmt"
	"io"
)

// Run executes the CLI and returns the process exit status.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		if _, err := fmt.Fprintln(stdout, "Usage: noma [--help]\n\nRun coding agents in remote workspaces.\nWorkspace commands are under development."); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(stderr, "unknown command; run noma --help")
	return 1
}
