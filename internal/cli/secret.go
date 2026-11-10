package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"

	"github.com/MrDHat/noma/internal/api"
)

const secretUsage = `usage:
  noma secret set NAME [--git-host HOST] [--env VAR] [--repos PATTERN]...
                                    store a secret, read from a hidden prompt or stdin
  noma secret ls                    list your secrets (never their values)
  noma secret rm NAME               delete a secret`

func secret(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New(secretUsage)
	}
	switch args[0] {
	case "set":
		return secretSet(ctx, args[1:], stdin, stdout)
	case "ls":
		return secretList(ctx, stdout)
	case "rm":
		if len(args) != 2 {
			return errors.New("usage: noma secret rm NAME")
		}
		return withClient(ctx, func(c *Client) error {
			if err := c.do(ctx, http.MethodDelete, "/v1/secrets/"+url.PathEscape(args[1]), nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "secret %s deleted\n", args[1])
			return nil
		})
	}
	return errors.New(secretUsage)
}

func secretSet(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("secret set", flag.ContinueOnError)
	var req api.SecretRequest
	fs.StringVar(&req.GitHost, "git-host", "", "deliver as the git credential for this host")
	fs.StringVar(&req.Env, "env", "", "deliver as this environment variable")
	fs.Func("repos", "only for repos matching this pattern, like github.com/acme/* (repeatable)", func(p string) error {
		req.Repos = append(req.Repos, p)
		return nil
	})
	// Take the name before or after the flags.
	var name string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if name == "" && fs.NArg() == 1 {
		name = fs.Arg(0)
	} else if name == "" || fs.NArg() != 0 {
		return errors.New(secretUsage)
	}
	value, err := readSecret(stdin, stdout, name)
	if err != nil {
		return err
	}
	req.Value = value
	return withClient(ctx, func(c *Client) error {
		if err := c.do(ctx, http.MethodPut, "/v1/secrets/"+url.PathEscape(name), req, nil); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "secret %s saved\n", name)
		return nil
	})
}

// readSecret reads the value from a hidden prompt, or from stdin when it
// isn't a terminal. It is never taken from the command line, so it stays
// out of shell history.
func readSecret(stdin io.Reader, stdout io.Writer, name string) (string, error) {
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprintf(stdout, "Value for %s (hidden): ", name)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(stdout)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	b, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
	if err != nil {
		return "", err
	}
	value := strings.TrimRight(string(b), "\r\n")
	if value == "" {
		return "", errors.New("no value on stdin")
	}
	return value, nil
}

func secretList(ctx context.Context, stdout io.Writer) error {
	return withClient(ctx, func(c *Client) error {
		var list api.SecretList
		if err := c.do(ctx, http.MethodGet, "/v1/secrets", nil, &list); err != nil {
			return err
		}
		if len(list.Secrets) == 0 {
			fmt.Fprintln(stdout, "no secrets; add one with noma secret set NAME --env VAR")
			return nil
		}
		tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tDELIVERY\tREPOS")
		for _, s := range list.Secrets {
			var to []string
			if s.GitHost != "" {
				to = append(to, "git:"+s.GitHost)
			}
			if s.Env != "" {
				to = append(to, "env:"+s.Env)
			}
			repos := strings.Join(s.Repos, ",")
			if repos == "" {
				repos = "all"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, strings.Join(to, ","), repos)
		}
		return tw.Flush()
	})
}

// withClient connects to the server, runs f and disconnects.
func withClient(ctx context.Context, f func(*Client) error) error {
	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	return f(c)
}
