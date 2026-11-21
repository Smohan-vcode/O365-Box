package cli

import (
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Smohan-vcode/O365-Box/engine/internal/draft"
	"github.com/Smohan-vcode/O365-Box/engine/internal/tui"
)

type tuiFlags struct {
	theme     string
	plain     bool
	draftPath string
}

func (f *tuiFlags) bind(cmd *cobra.Command, persistent bool) {
	fs := cmd.Flags()
	if persistent {
		fs = cmd.PersistentFlags()
	}
	fs.StringVar(&f.theme, "theme", "auto", "colours: auto, dark, light or plain")
	fs.BoolVar(&f.plain, "plain", false, "plain mode: no colour or box drawing (screen readers)")
	fs.StringVar(&f.draftPath, "draft", "", "encrypted draft file (default: user config dir)")
}

func (f *tuiFlags) run() error {
	p := f.draftPath
	if p == "" {
		var err error
		if p, err = draft.DefaultPath(); err != nil {
			return err
		}
	}
	theme := f.theme
	if f.plain {
		theme = "plain"
	}
	return tui.Run(tui.Options{DraftPath: p, Theme: theme})
}

func isTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
}

func newOnboardCmd() *cobra.Command {
	f := &tuiFlags{}
	c := &cobra.Command{Use: "onboard", Short: "Start or continue the guided onboarding (the default when box runs with no arguments)",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !isTerminal() {
				return usageError("onboarding needs an interactive terminal")
			}
			return f.run()
		}}
	f.bind(c, false)
	return c
}
