package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/MrDHat/noma/internal/api"
	"github.com/MrDHat/noma/internal/version"
)

const usage = `usage:
  noma up [--name NAME] <git-url>   create a workspace for a public repo
  noma rm <name>                    delete a workspace and its volume
  noma version
`

// Run runs the noma command with args (without the program name).
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "version", "--version":
		fmt.Fprintln(stdout, "noma", version.Version)
		return 0
	case "up":
		err = up(ctx, args[1:], stdout)
	case "rm":
		err = rm(ctx, args[1:], stdout)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "noma:", err)
		return 1
	}
	return 0
}

func connect(ctx context.Context) (*Client, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	return Connect(ctx, cfg)
}

func up(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	name := fs.String("name", "", "workspace name (default: the repo name)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: noma up [--name NAME] <git-url>")
	}
	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	ws, err := c.CreateWorkspace(ctx, api.CreateWorkspaceRequest{RepoURL: fs.Arg(0), Name: *name})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "workspace %s created (%s)\n", ws.Name, ws.Phase)
	return nil
}

func rm(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: noma rm <name>")
	}
	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.DeleteWorkspace(ctx, args[0]); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "workspace %s deleted\n", args[0])
	return nil
}
