package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/MrDHat/noma/internal/api"
	"github.com/MrDHat/noma/internal/version"
)

const pollInterval = time.Second

const usage = `usage:
  noma connect [--context CTX]      point this CLI at a cluster's Noma install
  noma up [--name NAME] <git-url>   create a workspace for a public repo
  noma ls                           list your workspaces
  noma attach <name>                wait for a workspace and print its link
  noma stop <name>                  stop a workspace, keeping its files
  noma start <name>                 start a stopped workspace again
  noma rm <name>                    delete a workspace and its volume
  noma secret set|ls|rm             manage secrets delivered to your workspaces
  noma version
`

// Run runs the noma command with args (without the program name).
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "version", "--version":
		fmt.Fprintln(stdout, "noma", version.Version)
		return 0
	case "connect":
		err = connectCmd(ctx, args[1:], stdout)
	case "up":
		err = up(ctx, args[1:], newLines(stdin), stdout)
	case "ls":
		err = ls(ctx, args[1:], stdout)
	case "attach":
		err = attach(ctx, args[1:], newLines(stdin), stdout)
	case "rm":
		err = rm(ctx, args[1:], stdout)
	case "stop":
		err = stop(ctx, args[1:], stdout)
	case "start":
		err = start(ctx, args[1:], newLines(stdin), stdout)
	case "secret":
		err = secret(ctx, args[1:], stdin, stdout)
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

func up(ctx context.Context, args []string, stdin *lines, stdout io.Writer) error {
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
	git := laptopGitIdentity(ctx)
	if git == nil {
		fmt.Fprintln(stdout, "warning: git user.name or user.email is not set on this machine; workspaces keep the identity from your last noma up, if any")
	}
	ws, err := c.CreateWorkspace(ctx, api.CreateWorkspaceRequest{RepoURL: fs.Arg(0), Name: *name, Git: git})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "workspace %s created\n", ws.Name)
	return waitReady(ctx, c, ws.Name, stdin, stdout)
}

// laptopGitIdentity returns this machine's git user.name and user.email,
// or nil unless both are set.
func laptopGitIdentity(ctx context.Context) *api.GitIdentity {
	get := func(key string) string {
		out, _ := exec.CommandContext(ctx, "git", "config", "--get", key).Output()
		return strings.TrimSpace(string(out))
	}
	g := &api.GitIdentity{Name: get("user.name"), Email: get("user.email")}
	if g.Name == "" || g.Email == "" {
		return nil
	}
	return g
}

// waitReady prints progress until the workspace is ready, and gives up if
// it is stopped or deleted. Ctrl-C stops waiting; the workspace keeps going.
func waitReady(ctx context.Context, c *Client, name string, stdin *lines, stdout io.Writer) error {
	last, answered := "", ""
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
		case api.PhaseSignIn:
			if ws.SignIn != nil && ws.SignIn.PublicKey != answered {
				answered = ws.SignIn.PublicKey
				if err := signIn(ctx, c, name, ws.SignIn, stdin, stdout); err != nil {
					return fmt.Errorf("sign-in: %w", err)
				}
			}
		case api.PhaseReady:
			fmt.Fprintf(stdout, "\nWorkspace %s is ready. Open it in claude.ai/code or the Claude app:\n  %s\n", name, ws.Link)
			if h := silentHint(ws); h != "" {
				fmt.Fprintf(stdout, "Note: %s; the workspace may not be reachable.\n", h)
			}
			return nil
		case api.PhaseError:
			return fmt.Errorf("workspace %s failed: %s", name, ws.Message)
		case api.PhaseStopping, api.PhaseStopped:
			return fmt.Errorf("workspace %s is %s; run noma start %s", name, ws.Phase, name)
		case api.PhaseDeleting:
			return fmt.Errorf("workspace %s is being deleted", name)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("stopped waiting; workspace %s keeps starting", name)
		case <-time.After(pollInterval):
		}
	}
}

func progress(ws *api.Workspace) string {
	line := "  " + ws.Phase
	if ws.Message != "" {
		line += ": " + ws.Message
	}
	if h := silentHint(ws); h != "" {
		line += " (" + h + ")"
	}
	return line
}

// silentHint warns that the workspace's agent has stopped checking in, so
// its phase may be out of date.
func silentHint(ws *api.Workspace) string {
	if ws.AgentSilentSeconds == 0 {
		return ""
	}
	return fmt.Sprintf("agent silent for %dm", ws.AgentSilentSeconds/60)
}

func ls(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: noma ls")
	}
	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	list, err := c.Workspaces(ctx)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(stdout, "no workspaces; create one with noma up <git-url>")
		return nil
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tPHASE\tRESTARTS\tREPO\tLINK\tNOTE")
	for _, ws := range list {
		link := ws.Link
		if link == "" {
			link = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n", ws.Name, ws.Phase, ws.ClaudeRestarts, ws.RepoURL, link, silentHint(&ws))
	}
	return tw.Flush()
}

// attach picks up a workspace where noma up left it: it shows progress,
// asks for a pending sign-in and prints the link.
func attach(ctx context.Context, args []string, stdin *lines, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: noma attach <name>")
	}
	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return waitReady(ctx, c, args[0], stdin, stdout)
}

func stop(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: noma stop <name>")
	}
	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.StopWorkspace(ctx, args[0]); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "stopping workspace %s…\n", args[0])
	// The server removes the pod and keeps the volume.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	for {
		ws, err := c.Workspace(ctx, args[0])
		if err != nil {
			return err
		}
		if ws.Phase == api.PhaseStopped {
			fmt.Fprintf(stdout, "workspace %s stopped; its files are kept\n", args[0])
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("workspace %s is still stopping", args[0])
		case <-time.After(pollInterval):
		}
	}
}

func start(ctx context.Context, args []string, stdin *lines, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: noma start <name>")
	}
	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.StartWorkspace(ctx, args[0]); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "starting workspace %s\n", args[0])
	return waitReady(ctx, c, args[0], stdin, stdout)
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
