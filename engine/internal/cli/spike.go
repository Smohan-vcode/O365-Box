package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Smohan-vcode/O365-Box/engine/internal/auth"
	"github.com/Smohan-vcode/O365-Box/engine/internal/bootstrap"
	"github.com/Smohan-vcode/O365-Box/engine/internal/discovery"
	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
)

// exchangeAdminRole is the Entra "Exchange Administrator" role template.
const exchangeAdminRole = "29232cdf-9323-42fd-ade2-1d097af3e4de"

type spikeStep struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

// newSpikeCmd runs spikes S-08 (discovery) and S-06 (bootstrap actions)
// against a test tenant. Hidden: a development tool, not a product command.
func newSpikeCmd(g *globals) *cobra.Command {
	var tenant, out string
	var write bool
	c := &cobra.Command{Use: "spike-tenant", Hidden: true, Short: "Run spikes S-08 and S-06 against a test tenant", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			root, err := g.root()
			if err != nil {
				return err
			}
			if out == "" {
				return usageError("--out is required (a file outside the repository)")
			}
			if rel, inside := relInside(root, out); inside {
				return usageError("--out must be outside the repository (it holds tenant data): " + rel)
			}
			var scopes struct {
				Delegated []string `yaml:"delegated"`
			}
			if err := readYAMLFile(filepath.Join(root, "expected-state", "bootstrap-scopes.yaml"), &scopes); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
			defer cancel()
			res := map[string]any{"started": time.Now().UTC()}
			steps := []spikeStep{}
			rec := func(name string, err error, detail string) bool {
				s := spikeStep{Name: name, OK: err == nil, Detail: detail}
				if err != nil {
					s.Error = err.Error()
				}
				steps = append(steps, s)
				mark := "✓"
				if err != nil {
					mark = "✗"
				}
				fmt.Fprintf(w, "%s %s %s %s\n", mark, name, detail, s.Error)
				return err == nil
			}
			save := func() {
				res["steps"] = steps
				res["finished"] = time.Now().UTC()
				b, _ := json.MarshalIndent(res, "", "  ")
				_ = os.WriteFile(out, b, 0o600)
			}
			defer save()

			t0 := time.Now()
			tok, err := auth.SignInDeviceCode(ctx, nil, tenant, auth.GraphCLIClientID, auth.GraphScopes(scopes.Delegated), func(dc auth.DeviceCode) {
				fmt.Fprintf(w, "SIGN-IN: open %s and enter the code %s (expires in %d minutes)\n", dc.VerificationURI, dc.UserCode, dc.ExpiresIn/60)
			})
			if !rec("sign-in (device code)", err, "") {
				return exitError{ExitFailure}
			}
			res["sign_in_seconds"] = int(time.Since(t0).Seconds())
			res["token_tenant_matches"] = tok.TenantID() == tenant
			res["granted_scopes"] = tok.Scopes()
			rec("token tenant matches", boolErr(tok.TenantID() == tenant, "token is for another tenant"), "")
			gc := &graph.Client{Token: func() string { return tok.Access }}

			// S-08: discovery.
			d, err := discovery.Discover(ctx, gc)
			if rec("discovery", err, "") {
				res["discovery"] = d
				res["organization_fields"] = d.Organization.Fields()
			}
			if !write {
				return nil
			}

			// S-06: bootstrap actions on a throwaway app, then clean up.
			suffix := randHex(4)
			app, err := bootstrap.CreateApp(ctx, gc, "box-spike-s06-"+suffix)
			if !rec("create application", err, "box-spike-s06-"+suffix) {
				return exitError{ExitFailure}
			}
			defer func() {
				cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer ccancel()
				rec("cleanup: delete and purge application", bootstrap.DeleteApp(cctx, gc, app), "")
			}()
			if rec("create service principal", bootstrap.CreateServicePrincipal(ctx, gc, app), "") {
				time.Sleep(10 * time.Second) // let the service principal replicate
				id, err := bootstrap.GrantAppRole(ctx, gc, app, "Organization.Read.All")
				rec("grant application permission (admin consent)", err, "Organization.Read.All")
				_ = id
				rid, err := bootstrap.AssignDirectoryRole(ctx, gc, app.SPID, exchangeAdminRole)
				if rec("assign Entra role", err, "Exchange Administrator") {
					defer func() {
						cctx, ccancel := context.WithTimeout(context.Background(), time.Minute)
						defer ccancel()
						rec("cleanup: remove role assignment", gc.Do(cctx, "DELETE", "/roleManagement/directory/roleAssignments/"+rid, nil, nil), "")
					}()
				}
			}
			rec("add GitHub federated credential", bootstrap.AddFederatedCredential(ctx, gc, app, "github-main", "repo:example-owner/example-repo:ref:refs/heads/main"), "")
			der, _, err := bootstrap.SelfSignedCert("box-spike-s06")
			if rec("generate certificate", err, "private key kept in memory only, then discarded") {
				rec("add certificate", bootstrap.AddCertificate(ctx, gc, app, der, "box-spike"), "")
			}
			secrets, certs, err := bootstrap.Credentials(ctx, gc, app)
			rec("check credentials", err, fmt.Sprintf("client secrets=%d certificates=%d", secrets, certs))
			res["client_secrets"], res["certificates"] = secrets, certs

			domain := "box-spike-" + suffix + ".example"
			recs, err := bootstrap.AddDomain(ctx, gc, domain)
			if rec("add domain and read verification records", err, domain) {
				types := []string{}
				for _, r := range recs {
					types = append(types, r.RecordType)
				}
				res["domain_verification_record_types"] = types
				rec("verify domain (expected to fail: no DNS record)", bootstrap.VerifyDomain(ctx, gc, domain), "")
				rec("cleanup: delete domain", bootstrap.DeleteDomain(ctx, gc, domain), "")
			}
			return nil
		}}
	c.Flags().StringVar(&tenant, "tenant", "", "test tenant id")
	c.Flags().StringVar(&out, "out", "", "results file, outside the repository")
	c.Flags().BoolVar(&write, "write", false, "also run the S-06 bootstrap actions (creates and deletes a throwaway app)")
	_ = c.MarkFlagRequired("tenant")
	return c
}

func boolErr(ok bool, msg string) error {
	if ok {
		return nil
	}
	return fmt.Errorf("%s", msg)
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
