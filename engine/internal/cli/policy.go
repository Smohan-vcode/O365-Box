package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
	"github.com/Smohan-vcode/O365-Box/engine/internal/report"
)

func newPolicyCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "policy", Short: "Work with the policy catalogue (policy/) and a company's selection"}

	cmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Check the policy catalogue's rules",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := g.root()
			if err != nil {
				return err
			}
			cat, issues, err := loadCatalogue(root)
			if err != nil {
				return err
			}
			res := report.Result{Command: "policy validate", OK: len(issues) == 0, Issues: issues}
			res.Summary = fmt.Sprintf("%d requirements, %d settings in %d centres, %d frameworks",
				len(cat.Requirements), cat.SettingCount(), len(cat.Centres), len(cat.Frameworks))
			if !res.OK {
				res.Summary = fmt.Sprintf("%d problem(s) in policy/", len(issues))
			}
			if err := res.Write(cmd.OutOrStdout(), g.asJSON); err != nil {
				return err
			}
			if !res.OK {
				return exitError{ExitValidation}
			}
			return nil
		},
	})

	var rf resolveFlags
	resolve := &cobra.Command{
		Use:   "resolve",
		Short: "Resolve the configuration's policy selection into the settings to apply",
		Long: "Resolve keeps a setting when the plan has its capability, its tier is selected and no\n" +
			"exception removes it. --out writes <centre>.yaml, not-applied.yaml and coverage.md into a\n" +
			"directory outside the repository: the output describes one company and is never committed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, cat, res, err := rf.resolve(g, cmd)
			if err != nil || res == nil {
				return err
			}
			if rf.out != "" {
				dir, err := outsideRepo(root, rf.out)
				if err != nil {
					return err
				}
				names, err := res.WriteDir(cat, dir)
				if err != nil {
					return err
				}
				if !g.asJSON {
					fmt.Fprintf(cmd.OutOrStdout(), "  wrote %s: %s\n", dir, strings.Join(names, ", "))
				}
			}
			if g.asJSON {
				return writeJSON(cmd.OutOrStdout(), res)
			}
			w := cmd.OutOrStdout()
			for _, ac := range res.Applied {
				modes := map[string]int{}
				for _, s := range ac.Settings {
					modes[s.Mode]++
				}
				fmt.Fprintf(w, "  %-16s %3d applied (%s)\n", ac.ID, len(ac.Settings), modeList(modes))
			}
			fmt.Fprintf(w, "✓ policy resolve: plan %s, tier %s, stage %s: %s\n",
				res.Plan, res.Selection.Tier, res.Selection.Stage, res.Summary())
			return nil
		},
	}
	rf.bind(resolve)
	resolve.Flags().StringVar(&rf.out, "out", "", "write the resolved files into this directory (outside the repository)")
	cmd.AddCommand(resolve)

	var cf resolveFlags
	coverage := &cobra.Command{
		Use:   "coverage",
		Short: "Print the coverage report for the configuration's selection as Markdown",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, cat, res, err := cf.resolve(g, cmd)
			if err != nil || res == nil {
				return err
			}
			if g.asJSON {
				return writeJSON(cmd.OutOrStdout(), res.Coverage)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), res.CoverageMarkdown(cat))
			return err
		},
	}
	cf.bind(coverage)
	cmd.AddCommand(coverage)
	return cmd
}

type resolveFlags struct {
	config string
	plan   string
	out    string
}

func (f *resolveFlags) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.config, "config", "", "the configuration document (required)")
	cmd.Flags().StringVar(&f.plan, "plan", "", "plan to resolve for (default: tenant.default_plan)")
	_ = cmd.MarkFlagRequired("config")
}

// resolve validates the catalogue and the configuration, then resolves.
// A nil result with a nil error means problems were printed and the exit
// code is set.
func (f *resolveFlags) resolve(g *globals, cmd *cobra.Command) (string, *policy.Catalogue, *policy.Result, error) {
	root, err := g.root()
	if err != nil {
		return "", nil, nil, err
	}
	cat, issues, err := loadCatalogue(root)
	if err != nil {
		return "", nil, nil, err
	}
	doc, err := config.Load(f.config)
	if err != nil {
		return "", nil, nil, err
	}
	if len(issues) == 0 {
		opt, err := engineOptions(root)
		if err != nil {
			return "", nil, nil, err
		}
		if issues, err = config.Validate(doc, opt); err != nil {
			return "", nil, nil, err
		}
	}
	name := "policy " + cmd.Name()
	if len(issues) > 0 {
		res := report.Result{Command: name, Issues: issues,
			Summary: fmt.Sprintf("%d problem(s); fix them before resolving", len(issues))}
		if err := res.Write(cmd.OutOrStdout(), g.asJSON); err != nil {
			return "", nil, nil, err
		}
		return "", nil, nil, exitError{ExitValidation}
	}
	plans, err := enginePlans(root)
	if err != nil {
		return "", nil, nil, err
	}
	planName := f.plan
	if planName == "" {
		planName = doc.Config.Tenant.DefaultPlan
	}
	list, ok := plans[planName]
	if !ok {
		return "", nil, nil, usageError(fmt.Sprintf("engine/capabilities.yaml has no plan %q", planName))
	}
	caps := map[string]bool{}
	for _, c := range list {
		caps[c] = true
	}
	res, err := cat.Resolve(selection(doc.Config.Policy), planName, caps)
	if err != nil {
		return "", nil, nil, err
	}
	return root, cat, res, nil
}

func selection(p config.Policy) policy.Selection {
	sel := policy.Selection{Frameworks: p.Frameworks, Tier: p.Tier, Stage: p.Rollout.Stage,
		Exceptions: []policy.Exception{}}
	if sel.Frameworks == nil {
		sel.Frameworks = []string{}
	}
	for _, e := range p.Exceptions {
		sel.Exceptions = append(sel.Exceptions, policy.Exception{ID: e.ID, Reason: e.Reason, Reference: e.Reference, Ends: e.Ends})
	}
	return sel
}

// loadCatalogue loads policy/ and checks it against engine/capabilities.yaml.
func loadCatalogue(root string) (*policy.Catalogue, []report.Issue, error) {
	cat, err := policy.Load(filepath.Join(root, "policy"), "policy")
	if err != nil {
		return nil, nil, err
	}
	plans, err := enginePlans(root)
	if err != nil {
		return nil, nil, err
	}
	issues := cat.Validate(plans)
	// Every capability a plan includes needs the service plans that provide
	// it, so the tenant check can confirm it (T14-02).
	sp, err := servicePlans(root)
	if err != nil {
		return nil, nil, err
	}
	for key, c := range cat.Capabilities {
		if key == "free" || len(c.IncludedIn) == 0 {
			continue
		}
		if len(sp[key]) == 0 {
			issues = append(issues, report.Issue{Check: "policy", File: "engine/capabilities.yaml",
				Message: fmt.Sprintf("capability %q has no service_plans entry", key)})
		}
	}
	return cat, issues, nil
}

func enginePlans(root string) (map[string][]string, error) {
	var file struct {
		Plans map[string][]string `yaml:"plans"`
	}
	if err := readYAMLFile(filepath.Join(root, "engine", "capabilities.yaml"), &file); err != nil {
		return nil, err
	}
	return file.Plans, nil
}

// outsideRepo returns dir as an absolute path, refusing one inside the
// repository: resolved output describes one company (R1).
func outsideRepo(root, dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, abs)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", usageError(fmt.Sprintf("--out %s is inside the repository; write company output elsewhere", dir))
	}
	return abs, nil
}

func modeList(m map[string]int) string {
	var parts []string
	for _, k := range []string{policy.ModeEnforce, policy.ModeAudit, policy.ModeReport} {
		if m[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", m[k], k))
		}
	}
	return strings.Join(parts, ", ")
}
