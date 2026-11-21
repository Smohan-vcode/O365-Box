package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Smohan-vcode/O365-Box/engine/internal/choices"
	"github.com/Smohan-vcode/O365-Box/engine/internal/draft"
	"github.com/Smohan-vcode/O365-Box/engine/internal/repo"
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
	opt := tui.Options{DraftPath: p, Theme: theme}
	// The choices catalogue is engine data; until it is embedded in the
	// binary it is read from the repository when box runs inside one.
	if wd, err := os.Getwd(); err == nil {
		if root, err := repo.FindRoot(wd); err == nil {
			if cat, err := choices.Load(filepath.Join(root, "defaults", "choices.yaml")); err == nil {
				opt.Choices = cat
			}
			if areas, err := tui.LoadAreas(filepath.Join(root, "defaults", "areas.yaml")); err == nil {
				opt.Areas = areas
				if opt.Choices != nil {
					opt.AreaInfo = areaInfo(root, areas, opt.Choices)
				}
			}
		}
	}
	return tui.Run(opt)
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
