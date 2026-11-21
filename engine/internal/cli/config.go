package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
	"github.com/Smohan-vcode/O365-Box/engine/internal/report"
	"github.com/Smohan-vcode/O365-Box/engine/internal/tasks"
)

func newConfigCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Work with the configuration document"}
	var forTask string
	validate := &cobra.Command{
		Use:   "validate <file>",
		Short: "Validate a configuration document against the schema and the engine's rules",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := g.root()
			if err != nil {
				return err
			}
			doc, err := config.Load(args[0])
			if err != nil {
				return err
			}
			opt, err := engineOptions(root)
			if err != nil {
				return err
			}
			issues, err := config.Validate(doc, opt)
			if err != nil {
				return err
			}
			if forTask != "" {
				t, err := tasks.Find(root, forTask)
				if err != nil {
					return err
				}
				more, err := requiredIssues(doc, t)
				if err != nil {
					return err
				}
				issues = append(issues, more...)
			}
			res := report.Result{Command: "config validate", OK: len(issues) == 0, Issues: issues}
			res.Summary = fmt.Sprintf("%s is valid", doc.File)
			if forTask != "" {
				res.Summary += " for " + forTask
			}
			if !res.OK {
				res.Summary = fmt.Sprintf("%d problem(s) in %s", len(issues), doc.File)
			}
			if err := res.Write(cmd.OutOrStdout(), g.asJSON); err != nil {
				return err
			}
			if !res.OK {
				return exitError{ExitValidation}
			}
			return nil
		},
	}
	validate.Flags().StringVar(&forTask, "for", "", "also require the values a task lists under config_required")
	cmd.AddCommand(validate)
	return cmd
}

func requiredIssues(doc *config.Document, t *tasks.Task) ([]report.Issue, error) {
	var out []report.Issue
	for _, p := range t.ConfigRequired {
		vals, err := config.Resolve(doc.Raw, p)
		if err != nil {
			return nil, fmt.Errorf("%s: config_required: %w", t.File, err)
		}
		for _, v := range vals {
			if config.IsEmpty(v.Value) {
				out = append(out, report.Issue{Check: "required", Path: v.Path, File: doc.File, Line: doc.Line(v.Path),
					Message: fmt.Sprintf("required by %s but empty", t.ID)})
			}
		}
	}
	return out, nil
}

// engineOptions loads the engine data the semantic checks compare against.
func engineOptions(root string) (config.Options, error) {
	var opt config.Options
	var roles struct {
		Roles []struct {
			Name string `yaml:"name"`
		} `yaml:"roles"`
	}
	if err := readYAMLFile(filepath.Join(root, "defaults", "roles.yaml"), &roles); err != nil {
		return opt, err
	}
	opt.Roles = []string{}
	for _, r := range roles.Roles {
		opt.Roles = append(opt.Roles, r.Name)
	}
	catPath := filepath.Join(root, "baseline", "catalogue.yaml")
	if _, err := os.Stat(catPath); err == nil {
		var cat struct {
			Settings []struct {
				ID string `yaml:"id"`
			} `yaml:"settings"`
		}
		if err := readYAMLFile(catPath, &cat); err != nil {
			return opt, err
		}
		opt.Settings = []string{}
		for _, s := range cat.Settings {
			opt.Settings = append(opt.Settings, s.ID)
		}
	}
	jdir := filepath.Join(root, "defaults", "jurisdictions")
	if entries, err := os.ReadDir(jdir); err == nil {
		opt.Jurisdictions = []string{}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				opt.Jurisdictions = append(opt.Jurisdictions, strings.TrimSuffix(e.Name(), ".yaml"))
			}
		}
		sort.Strings(opt.Jurisdictions)
	}
	return opt, nil
}

func readYAMLFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
