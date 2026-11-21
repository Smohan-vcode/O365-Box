package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Smohan-vcode/O365-Box/engine/internal/auth"
	"github.com/Smohan-vcode/O365-Box/engine/internal/bootstrap"
	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
	"github.com/Smohan-vcode/O365-Box/engine/internal/discovery"
	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
	"github.com/Smohan-vcode/O365-Box/engine/internal/keystore"
)

func openKeystore() (keystore.Store, error) {
	dir, err := keystore.DefaultDir()
	if err != nil {
		return nil, err
	}
	return keystore.Open(dir)
}

func loadValidConfig(root, path string, out io.Writer) (*config.Document, error) {
	if path == "" {
		return nil, usageError("--config is required until the tenant configuration store exists (T0-06)")
	}
	doc, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	opt, err := engineOptions(root)
	if err != nil {
		return nil, err
	}
	issues, err := config.Validate(doc, opt)
	if err != nil {
		return nil, err
	}
	for _, i := range issues {
		fmt.Fprintln(out, "  ✗ "+i.String())
	}
	if len(issues) > 0 {
		return nil, validationError(fmt.Sprintf("%d problem(s) in %s", len(issues), doc.File))
	}
	if doc.Config.Tenant.TenantID == "" {
		return nil, validationError("tenant.tenant_id is empty")
	}
	return doc, nil
}

func newBootstrapCmd(g *globals) *cobra.Command {
	var cfgPath, repo string
	var live bool
	c := &cobra.Command{Use: "bootstrap", Short: "Sign in as Global Administrator once and create the pipeline app, its permissions and the break-glass accounts (SPEC §18.5)",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			root, err := g.root()
			if err != nil {
				return err
			}
			doc, err := loadValidConfig(root, cfgPath, out)
			if err != nil {
				return err
			}
			cfg := doc.Config
			if cfg.Tenant.Environment == "live" && !live {
				return usageError("this configuration targets a live tenant: bootstrap the test tenant first, then pass --live")
			}
			gr, err := neededGrants(root, &cfg)
			if err != nil {
				return err
			}
			perms := gr.Permissions
			fmt.Fprintln(out, "Permission bundles this configuration needs (you approve them at the sign-in):")
			for _, n := range gr.Needs {
				fmt.Fprintln(out, "  "+n.describe())
			}
			ks, err := openKeystore()
			if err != nil {
				return err
			}
			var scopes struct {
				Delegated []string `yaml:"delegated"`
			}
			if err := readYAMLFile(filepath.Join(root, "expected-state", "bootstrap-scopes.yaml"), &scopes); err != nil {
				return err
			}
			var bg []bootstrap.BreakGlassUser
			for _, u := range cfg.Users {
				if u.BreakGlass {
					bg = append(bg, bootstrap.BreakGlassUser{Alias: u.Primary, DisplayName: strings.TrimSpace(u.GivenName + " " + u.FamilyName)})
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
			defer cancel()
			tenant := cfg.Tenant.TenantID
			tok, err := auth.SignInDeviceCode(ctx, nil, tenant, auth.GraphCLIClientID, auth.GraphScopes(scopes.Delegated), func(dc auth.DeviceCode) {
				fmt.Fprintf(out, "SIGN-IN: open %s and enter the code %s (expires in %d minutes)\n", dc.VerificationURI, dc.UserCode, dc.ExpiresIn/60)
			})
			if err != nil {
				return err
			}
			if tok.TenantID() != tenant {
				return fmt.Errorf("signed in to a different tenant than tenant.tenant_id")
			}
			fmt.Fprintln(out, "✓ signed in as Global Administrator")
			admin := &graph.Client{Token: func() string { return tok.Access }}
			if d, err := discovery.Discover(ctx, admin); err == nil {
				fmt.Fprintf(out, "✓ discovery: %d domain(s), %d plan(s), %d user(s), security defaults on=%v\n",
					len(d.Domains), len(d.Plans), d.Users, d.SecurityDefaultsOn != nil && *d.SecurityDefaultsOn)
			}
			say := func(f string, a ...any) { fmt.Fprintf(out, f+"\n", a...) }
			res, err := bootstrap.Run(ctx, admin, bootstrap.Options{Tenant: tenant, Permissions: perms, BreakGlass: bg,
				GitHubRepo: repo, PrimaryDomain: cfg.Company.PrimaryDomain, Keys: ks, Say: say})
			// R12: the Global Administrator token is dropped here and never stored.
			tok.Access, admin = "", nil
			fmt.Fprintln(out, "✓ Global Administrator session discarded")
			if err != nil {
				return err
			}
			for _, r := range res.DomainRecords {
				fmt.Fprintf(out, "  DNS %s %s %s ttl=%d\n", r.RecordType, r.Label, r.Text+r.MailExch, r.TTL)
			}
			return checkIdentity(ctx, out, tenant, ks, perms, 3*time.Minute)
		}}
	c.Flags().StringVar(&cfgPath, "config", "", "configuration document")
	c.Flags().StringVar(&repo, "github-repo", "", "owner/name of the company's copy of this repository, for the federated credential")
	c.Flags().BoolVar(&live, "live", false, "allow a live tenant")
	return c
}

// checkIdentity signs in as the pipeline app with its certificate and
// compares it with expected-state (box check identity). wait allows for
// a new certificate and grants to propagate.
func checkIdentity(ctx context.Context, out io.Writer, tenant string, ks keystore.Store, perms bootstrap.Permissions, wait time.Duration) error {
	cred, pl, err := bootstrap.Credential(ks, tenant)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(wait)
	var problems []string
	for {
		tok, terr := cred.Token(ctx, "https://graph.microsoft.com")
		if terr == nil {
			gc := &graph.Client{Token: func() string { return tok.Access }}
			problems, err = bootstrap.Check(ctx, gc, pl, perms)
			if err == nil && len(problems) == 0 {
				break
			}
		} else {
			err = terr
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(15 * time.Second):
		}
	}
	if err != nil {
		return fmt.Errorf("check identity: %w", err)
	}
	for _, p := range problems {
		fmt.Fprintln(out, "  ✗ "+p)
	}
	if len(problems) > 0 {
		for _, p := range problems {
			if strings.HasPrefix(p, "missing ") {
				fmt.Fprintln(out, "  The configuration needs permission bundles the app does not have yet: run box bootstrap again and approve them at the Global Administrator sign-in (SPEC §24.3).")
				break
			}
		}
		fmt.Fprintf(out, "✗ check identity: %d problem(s)\n", len(problems))
		return exitError{ExitFailure}
	}
	fmt.Fprintf(out, "✓ check identity: the pipeline app signs in with its certificate; permissions and roles equal expected-state; no client secret (thumbprint %s…)\n", pl.Thumbprint[:8])
	return nil
}
