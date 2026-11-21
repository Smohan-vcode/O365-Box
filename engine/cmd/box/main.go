// Command box is the Company in a Box orchestrator, CLI and TUI.
package main

import (
	"os"

	"github.com/Smohan-vcode/O365-Box/engine/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
