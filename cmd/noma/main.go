// Command noma is the Noma CLI.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/MrDHat/noma/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
