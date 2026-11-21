package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Smohan-vcode/O365-Box/engine/internal/doctor"
)

func newDoctorCmd(g *globals) *cobra.Command {
	var opt doctor.Options
	var install bool
	c := &cobra.Command{Use: "doctor", Short: "Check pwsh, the pinned Microsoft modules and tools", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := g.root()
			if err != nil {
				return err
			}
			pins, err := doctor.LoadPins(filepath.Join(root, "expected-state", "modules.yaml"))
			if err != nil {
				return err
			}
			ctx := context.Background()
			items := doctor.Check(ctx, pins, opt)
			if install && !doctor.OK(items) {
				fmt.Fprintln(cmd.OutOrStdout(), "Installing missing modules…")
				if err := doctor.Install(ctx, pins, opt, items); err != nil {
					return err
				}
				items = doctor.Check(ctx, pins, opt)
			}
			ok := doctor.OK(items)
			out := cmd.OutOrStdout()
			if g.asJSON {
				if err := writeJSON(out, map[string]any{"command": "doctor", "ok": ok, "items": items}); err != nil {
					return err
				}
			} else {
				for _, it := range items {
					mark := "✓"
					if !it.OK {
						mark = "✗"
						if !it.Required {
							mark = "!"
						}
					}
					fmt.Fprintf(out, "%s %-46s want %-10s found %s\n", mark, it.Name, it.Want, it.Found)
					if !it.OK && it.Fix != "" {
						fmt.Fprintf(out, "    fix: %s\n", it.Fix)
					}
				}
				if ok {
					fmt.Fprintln(out, "✓ doctor: ready")
				} else {
					fmt.Fprintln(out, "✗ doctor: missing prerequisites (run box doctor --install for modules)")
				}
			}
			if !ok {
				return exitError{ExitFailure}
			}
			return nil
		}}
	c.Flags().StringVar(&opt.Pwsh, "pwsh", "pwsh", "pwsh executable")
	c.Flags().StringVar(&opt.ModulePath, "module-path", os.Getenv("BOX_MODULE_PATH"), "PSModulePath to check and install into")
	c.Flags().BoolVar(&install, "install", false, "install missing modules from the PowerShell Gallery")
	return c
}
