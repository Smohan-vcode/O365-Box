package cli

import (
	"fmt"
	"github.com/Smohan-vcode/O365-Box/engine/internal/bootstrap"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Smohan-vcode/O365-Box/engine/internal/choices"
	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
	"github.com/Smohan-vcode/O365-Box/engine/internal/report"
)

// ChoiceModules are modules only a choice switches on (SPEC §24).
var ChoiceModules = []string{"hybrid", "azure"}

func newChoicesCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{Use: "choices", Short: "Work with the onboarding choices (defaults/choices.yaml)"}
	cmd.AddCommand(&cobra.Command{
		Use: "validate", Short: "Check the choices catalogue and every setting, variant and module it names",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := g.root()
			if err != nil {
				return err
			}
			cc, issues, err := loadChoices(root)
			if err != nil {
				return err
			}
			res := report.Result{Command: "choices validate", OK: len(issues) == 0, Issues: issues,
				Summary: fmt.Sprintf("%d choices", len(cc.Choices))}
			if err := res.Write(cmd.OutOrStdout(), g.asJSON); err != nil {
				return err
			}
			if !res.OK {
				return exitError{ExitValidation}
			}
			return nil
		}})

	cmd.AddCommand(&cobra.Command{
		Use: "show <id>", Short: "Show one choice: its question, options, costs and effects",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := g.root()
			if err != nil {
				return err
			}
			cc, err := choices.Load(filepath.Join(root, "defaults", "choices.yaml"))
			if err != nil {
				return err
			}
			ch := cc.Choice(args[0])
			if ch == nil {
				return usageError(fmt.Sprintf("unknown choice %q; see box choices list", args[0]))
			}
			if g.asJSON {
				return writeJSON(cmd.OutOrStdout(), ch)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s (%s step)\n%s\n", ch.ID, ch.Step, ch.Question)
			if ch.Help != "" {
				fmt.Fprintf(out, "%s\n", ch.Help)
			}
			if ch.When != "" {
				fmt.Fprintf(out, "Asked only when: %s\n", ch.When)
			}
			for _, o := range ch.Options {
				mark := " "
				if o.Recommended {
					mark = "*"
				}
				fmt.Fprintf(out, "\n %s %s: %s  [cost: %s]\n", mark, o.Value, o.Label, o.Cost)
				if o.Detail != "" {
					fmt.Fprintf(out, "     %s\n", o.Detail)
				}
				if o.RecommendedWhen != "" {
					fmt.Fprintf(out, "     recommended when: %s\n", o.RecommendedWhen)
				}
				e := o.Effects
				for _, l := range []struct{ k, v string }{{"modules", strings.Join(e.Modules, ", ")},
					{"leaves out", strings.Join(e.Exceptions, ", ")}, {"variants", variantList(e.Variants)},
					{"permission bundles", strings.Join(e.Bundles, ", ")}, {"manual steps", strings.Join(e.Gates, ", ")}} {
					if l.v != "" {
						fmt.Fprintf(out, "     %s: %s\n", l.k, l.v)
					}
				}
			}
			return nil
		}})

	var cfgPath string
	list := &cobra.Command{
		Use: "list", Short: "List the choices with each answer (from --config) or its default",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := g.root()
			if err != nil {
				return err
			}
			cc, err := choices.Load(filepath.Join(root, "defaults", "choices.yaml"))
			if err != nil {
				return err
			}
			ctx := choices.Context{Modules: map[string]bool{}}
			answers := map[string]string{}
			if cfgPath != "" {
				doc, err := config.Load(cfgPath)
				if err != nil {
					return err
				}
				ctx.Modules = doc.Config.Modules.Map()
				answers = doc.Config.Choices
			}
			r, err := cc.Resolve(answers, ctx)
			if err != nil {
				return usageError(err.Error())
			}
			if g.asJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"answers": r.Answers, "defaulted": r.Defaulted,
					"left_out": r.LeftOut, "variants": r.Variants, "modules": keys(r.Modules), "bundles": r.Bundles, "gates": keys(r.Gates)})
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "CHOICE\tSTEP\tANSWER\tOPTIONS")
			for _, ch := range cc.Choices {
				v, ok := r.Answers[ch.ID]
				if !ok {
					v = "(not asked)"
				} else if r.Defaulted[ch.ID] {
					v += " (default)"
				}
				var opts []string
				for _, o := range ch.Options {
					opts = append(opts, o.Value)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ch.ID, ch.Step, v, strings.Join(opts, " | "))
			}
			if err := w.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\nleft out %d setting(s), %d variant(s); bundles: %s; gates: %s\n",
				len(r.LeftOut), len(r.Variants), strings.Join(sortedKeys(r.Bundles), ", "), strings.Join(keys(r.Gates), ", "))
			return nil
		}}
	list.Flags().StringVar(&cfgPath, "config", "", "configuration document whose answers to show")
	cmd.AddCommand(list)

	var permCfg string
	perms := &cobra.Command{
		Use: "permissions", Short: "List the permission bundles the configuration needs, and why (SPEC §24.3)",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := g.root()
			if err != nil {
				return err
			}
			if permCfg == "" {
				return usageError("--config is required")
			}
			doc, err := config.Load(permCfg)
			if err != nil {
				return err
			}
			gr, err := neededGrants(root, &doc.Config)
			if err != nil {
				return err
			}
			if g.asJSON {
				return writeJSON(cmd.OutOrStdout(), gr)
			}
			for _, n := range gr.Needs {
				fmt.Fprintln(cmd.OutOrStdout(), "  "+n.describe())
			}
			p := gr.Permissions
			fmt.Fprintf(cmd.OutOrStdout(), "✓ %d bundle(s): %d Graph, %d Exchange, %d SharePoint permission(s), %d Entra role(s)\n",
				len(gr.Needs), len(p.Graph), len(p.Exchange), len(p.SharePoint), len(p.EntraRoles))
			return nil
		}}
	perms.Flags().StringVar(&permCfg, "config", "", "configuration document (required)")
	cmd.AddCommand(perms)
	return cmd
}

func variantList(m map[string]string) string {
	var out []string
	for s, v := range m {
		out = append(out, s+"="+v)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// loadChoices loads the catalogue and checks it against policy/ and the mappings.
func loadChoices(root string) (*choices.Catalogue, []report.Issue, error) {
	cc, err := choices.Load(filepath.Join(root, "defaults", "choices.yaml"))
	if err != nil {
		return nil, nil, err
	}
	cat, err := policy.Load(filepath.Join(root, "policy"), "policy")
	if err != nil {
		return nil, nil, err
	}
	maps, _, err := policy.LoadMappings(filepath.Join(root, "expected-state", "setting-ids"))
	if err != nil {
		return nil, nil, err
	}
	k := choices.Known{Settings: map[string]bool{}, Variants: map[string]map[string]bool{}, Modules: map[string]bool{}}
	_, settings, _ := cat.IDs()
	for _, s := range settings {
		k.Settings[s] = true
	}
	for id, m := range maps {
		k.Variants[id] = map[string]bool{}
		for v := range m.Variants {
			k.Variants[id][v] = true
		}
	}
	for m := range (config.Modules{}).Map() {
		k.Modules[m] = true
	}
	for _, m := range ChoiceModules {
		k.Modules[m] = true
	}
	bundles, err := bootstrap.LoadBundles(filepath.Join(root, "expected-state", "app-permissions.yaml"))
	if err != nil {
		return nil, nil, err
	}
	k.Bundles = map[string]bool{}
	for _, b := range bundles.Bundles {
		k.Bundles[b.ID] = true
	}
	var gates struct {
		Gates []struct {
			ID string `yaml:"id"`
		} `yaml:"gates"`
	}
	if err := readYAMLFile(filepath.Join(root, "engine", "gates.yaml"), &gates); err != nil {
		return nil, nil, err
	}
	k.Gates = map[string]bool{}
	for _, g := range gates.Gates {
		k.Gates[g.ID] = true
	}
	return cc, cc.Validate(k), nil
}
