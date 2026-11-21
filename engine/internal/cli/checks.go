package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Smohan-vcode/O365-Box/engine/internal/checks"
	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
	"github.com/Smohan-vcode/O365-Box/engine/internal/repo"
	"github.com/Smohan-vcode/O365-Box/engine/internal/report"
)

const (
	exampleConfig     = "examples/acme/box-config.yaml"
	exampleDictionary = "examples/acme/dictionary.yaml"
)

func newCheckLiteralsCmd(g *globals) *cobra.Command {
	var fromFile, dictFile, scanDir string
	var fromTenant bool
	cmd := &cobra.Command{
		Use:   "check-literals",
		Short: "Fail if a company value or a secret appears in the repository (rule R1)",
		Long: "Collects company values from a configuration document (default: the fictional\n" +
			"examples/acme, whose values must not appear outside examples/) and scans every text\n" +
			"file for them and for anything that looks like a secret.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if fromTenant {
				return errors.New("--from-tenant is available after task T0-06 (configuration store)")
			}
			root, err := g.root()
			if err != nil {
				return err
			}
			scanRoot := root
			if scanDir != "" {
				if scanRoot, err = filepath.Abs(scanDir); err != nil {
					return err
				}
			}
			cfgPath, dictPath := fromFile, dictFile
			example := cfgPath == ""
			if example {
				cfgPath = filepath.Join(root, exampleConfig)
				if dictPath == "" {
					dictPath = filepath.Join(root, exampleDictionary)
				}
			}
			doc, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			dict := map[string]string{}
			if dictPath != "" {
				if err := readYAMLFile(dictPath, &dict); err != nil {
					return err
				}
			}
			lits := checks.LiteralsFrom(doc, dict)
			skipFiles := map[string]bool{}
			for _, p := range []string{cfgPath, dictPath} {
				if p == "" {
					continue
				}
				if rel, ok := relInside(scanRoot, p); ok {
					skipFiles[rel] = true
				}
			}
			skip := func(rel string) bool {
				if skipFiles[rel] || filepath.Base(rel) == "testdata" {
					return true
				}
				return example && repo.HasPrefixDir(rel, "examples")
			}
			issues, n, err := checks.Literals(scanRoot, lits, checks.ScanOptions{Skip: skip})
			if err != nil {
				return err
			}
			source := "examples/acme"
			if !example {
				source = fromFile
			}
			res := report.Result{Command: "check-literals", OK: len(issues) == 0, Issues: issues,
				Summary: fmt.Sprintf("%d files scanned for %d values from %s and for secrets", n, len(lits), source)}
			if !res.OK {
				res.Summary = fmt.Sprintf("%d finding(s) in %d files scanned", len(issues), n)
			}
			if err := res.Write(cmd.OutOrStdout(), g.asJSON); err != nil {
				return err
			}
			if !res.OK {
				return exitError{ExitFailure}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "take company values from this configuration document")
	cmd.Flags().StringVar(&dictFile, "dictionary", "", "also take values from this dictionary file (key: value)")
	cmd.Flags().BoolVar(&fromTenant, "from-tenant", false, "take company values from the tenant's configuration store")
	cmd.Flags().StringVar(&scanDir, "dir", "", "scan this directory instead of the repository")
	return cmd
}

func relInside(root, p string) (string, bool) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func newCheckConnectorsCmd(g *globals) *cobra.Command {
	p := checks.DefaultConnectorPaths
	cmd := &cobra.Command{
		Use:   "check-connectors",
		Short: "Fail if a flow uses a connector outside the standard allow-list (rule R2)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := g.root()
			if err != nil {
				return err
			}
			issues, n, err := checks.Connectors(root, p)
			if err != nil {
				return err
			}
			res := report.Result{Command: "check-connectors", OK: len(issues) == 0, Issues: issues,
				Summary: fmt.Sprintf("%d connector uses checked against %s", n, p.AllowList)}
			if !res.OK {
				res.Summary = fmt.Sprintf("%d connector(s) not allowed", len(issues))
			}
			if err := res.Write(cmd.OutOrStdout(), g.asJSON); err != nil {
				return err
			}
			if !res.OK {
				return exitError{ExitFailure}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&p.AllowList, "allow-list", p.AllowList, "allow-list file, relative to the repository")
	cmd.Flags().StringVar(&p.Catalogue, "catalogue", p.Catalogue, "flow catalogue, relative to the repository")
	cmd.Flags().StringVar(&p.Solution, "solution", p.Solution, "solution source directory, relative to the repository")
	return cmd
}
