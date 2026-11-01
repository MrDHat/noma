// Command noma-server is part of Noma.
package main

import (
	"fmt"
	"os"

	"github.com/MrDHat/noma/internal/version"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println("noma-server", version.Version)
		return
	}
	fmt.Fprintln(os.Stderr, "usage: noma-server version")
	os.Exit(2)
}
