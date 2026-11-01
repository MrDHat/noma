package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/MrDHat/noma/internal/api"
	"github.com/MrDHat/noma/internal/version"
)

const pollInterval = time.Second

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
	fmt.Fprintf(stdout, "workspace %s created\n", ws.Name)
	return waitReady(ctx, c, ws.Name, stdout)
}

// waitReady prints progress until the workspace is ready. Ctrl-C stops
// waiting; the workspace keeps going.
func waitReady(ctx context.Context, c *Client, name string, stdout io.Writer) error {
	last := ""
	for {
		ws, err := c.Workspace(ctx, name)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("stopped waiting; workspace %s keeps starting", name)
			}
			return err
		}
		if line := progress(ws); line != last {
			fmt.Fprintln(stdout, line)
			last = line
		}
		switch ws.Phase {
		case api.PhaseReady:
			return nil
		case api.PhaseError:
			return fmt.Errorf("workspace %s failed: %s", name, ws.Message)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("stopped waiting; workspace %s keeps starting", name)
		case <-time.After(pollInterval):
		}
	}
}

func progress(ws *api.Workspace) string {
	if ws.Message == "" {
		return "  " + ws.Phase
	}
	return fmt.Sprintf("  %s: %s", ws.Phase, ws.Message)
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
	fmt.Fprintf(stdout, "deleting workspace %s…\n", args[0])
	// The server removes the pod and volume, then the record.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	for {
		_, err := c.Workspace(ctx, args[0])
		if errors.Is(err, ErrNotFound) {
			fmt.Fprintf(stdout, "workspace %s deleted\n", args[0])
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("workspace %s is still being deleted", args[0])
		case <-time.After(pollInterval):
		}
	}
}
