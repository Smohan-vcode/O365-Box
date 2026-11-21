// Package cli wires the box commands (SPEC §18.7).
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/Smohan-vcode/O365-Box/engine/internal/repo"
)

// Exit codes (SPEC §18.7).
const (
	ExitOK         = 0
	ExitFailure    = 1
	ExitValidation = 2
	ExitGate       = 3
)

// exitError carries an exit code out of a command.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

type globals struct {
	repo   string
	asJSON bool
}

func (g *globals) root() (string, error) {
	if g.repo != "" {
		return repo.FindRoot(g.repo)
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return repo.FindRoot(wd)
}

// Version is set at build time with -ldflags "-X .../cli.Version=...".
var Version = "dev"

// New builds the root command writing to out and errOut.
func New(out, errOut io.Writer) *cobra.Command {
	g := &globals{}
	root := &cobra.Command{
		Use:   "box",
		Short: "Company in a Box: set up and harden a company on Microsoft 365",
		Long: "box sets up and hardens a company's Microsoft 365 tenant from a guided onboarding.\n" +
			"Run it with no arguments to start onboarding (available from task T13-01).",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.PersistentFlags().StringVar(&g.repo, "repo", "", "path inside the engine repository (default: current directory)")
	root.PersistentFlags().BoolVar(&g.asJSON, "json", false, "print machine-readable JSON")
	root.AddCommand(newConfigCmd(g), newCheckLiteralsCmd(g), newCheckConnectorsCmd(g), newBacklogCmd(g))
	root.AddCommand(newRunCommands(g)...)
	return root
}

// Run executes box with args and returns the process exit code.
func Run(args []string, out, errOut io.Writer) int {
	cmd := New(out, errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	if err == nil {
		return ExitOK
	}
	var ee exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	var ue usageErr
	if errors.As(err, &ue) {
		fmt.Fprintln(errOut, "box:", ue.msg)
		return ExitValidation
	}
	fmt.Fprintln(errOut, "box:", err)
	return ExitFailure
}
