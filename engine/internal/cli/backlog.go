package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Smohan-vcode/O365-Box/engine/internal/tasks"
)

func newBacklogCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "backlog", Short: "Work with the engine's development backlog (tasks/ and spikes/)"}
	load := func() (string, map[string]*tasks.Task, error) {
		root, err := g.root()
		if err != nil {
			return "", nil, err
		}
		all, err := tasks.LoadAll(root)
		return root, all, err
	}
	cmd.AddCommand(&cobra.Command{Use: "index", Short: "Regenerate tasks/INDEX.md", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, all, err := load()
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(root, "tasks", "INDEX.md"), []byte(tasks.Index(all)), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ tasks/INDEX.md: %d entries\n", len(all))
			return nil
		}})
	cmd.AddCommand(&cobra.Command{Use: "check", Short: "Fail if tasks/INDEX.md is out of date", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, all, err := load()
			if err != nil {
				return err
			}
			b, err := os.ReadFile(filepath.Join(root, "tasks", "INDEX.md"))
			if err != nil {
				return err
			}
			if string(b) != tasks.Index(all) {
				fmt.Fprintln(cmd.OutOrStdout(), "✗ backlog check: tasks/INDEX.md is out of date; run box backlog index")
				return exitError{ExitFailure}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ backlog check: %d entries, index up to date\n", len(all))
			return nil
		}})
	cmd.AddCommand(&cobra.Command{Use: "done <id>", Short: "Mark a task DONE (refused while a dependency is not DONE)",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, all, err := load()
			if err != nil {
				return err
			}
			if err := tasks.MarkDone(all, args[0]); err != nil {
				return usageError(err.Error())
			}
			if err := os.WriteFile(filepath.Join(root, "tasks", "INDEX.md"), []byte(tasks.Index(all)), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ %s is DONE; tasks/INDEX.md updated\n", args[0])
			return nil
		}})
	return cmd
}
