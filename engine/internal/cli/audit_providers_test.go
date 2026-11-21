package cli

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/audit"
	"github.com/Smohan-vcode/O365-Box/engine/internal/auth"
	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
)

// TestAuditRunsSharePointAndTeamsReadOnly runs the real policy.sharepoint and
// policy.teams verify in audit mode, in read-only workers, against stand-ins
// for PnP.PowerShell and MicrosoftTeams (internal/providers/testdata): each
// setting is read pass, fail or not read with the reason, and no cmdlet that
// writes is reached.
func TestAuditRunsSharePointAndTeamsReadOnly(t *testing.T) {
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh not installed")
	}
	root := "../../.."
	cat, _, err := loadCatalogue(root)
	if err != nil {
		t.Fatal(err)
	}
	maps, _, err := policy.LoadMappings(filepath.Join(root, "expected-state", "setting-ids"))
	if err != nil {
		t.Fatal(err)
	}
	keep := map[string]bool{"TMS-004": true, "TMS-005": true, "TMS-009": true, "TMS-012": true,
		"SPO-001": true, "SPO-002": true, "SPO-003": true, "SPO-004": true, "SPO-007": true, "SPO-008": true, "SPO-009": true, "SPO-013": true, "SPO-016": true, "SPO-018": true}
	all := audit.AuditSelection(cat, maps)
	bySteps := map[string][]policy.StepSetting{}
	for _, st := range []string{"policy.teams", "policy.sharepoint"} {
		for _, s := range all[st] {
			if keep[s.ID] {
				bySteps[st] = append(bySteps[st], s)
			}
		}
	}
	claims := map[string]any{"tid": "t", "oid": "o", "wids": []any{audit.RoleGlobalReader}}
	sess := audit.NewSession("t", "reader", nil, claims)
	src := audit.TokenSource{SignIn: func(_ context.Context, client string, scopes []string, _ string) (*auth.Token, error) {
		return &auth.Token{Access: "tok-" + client + "-0123456789", Claims: claims, ExpiresAt: time.Now().Add(time.Hour)}, nil
	}}
	toks := audit.NewTokens(sess, auth.GraphCLIClientID, nil, src)
	scopes, err := audit.LoadScopes(filepath.Join(root, audit.ScopesFile))
	if err != nil {
		t.Fatal(err)
	}
	toks.Acquire(context.Background(), scopes.Resources, "https://fernhilllabs-admin.sharepoint.com", nil)
	tenant := map[string]any{"id": "t", "initial_domain": "fernhilllabs.onmicrosoft.com",
		"sharepoint": "https://fernhilllabs.sharepoint.com", "sharepoint_admin": "https://fernhilllabs-admin.sharepoint.com"}
	f := &auditFlags{modulePath: "../providers/testdata/readonly/modules", timeout: time.Minute}
	outcomes := runAuditSteps(context.Background(), root, f, io.Discard, bySteps, tenant, auditCreds{graph: "graph-token-0123456789", tokens: toks})
	for _, st := range []string{"policy.teams", "policy.sharepoint"} {
		if o := outcomes[st]; !o.Ran || strings.Contains(string(o.Verify), "WROTE") {
			t.Fatalf("%s: %+v", st, o)
		}
	}
	reads := audit.ReadSettings(cat, maps, bySteps, outcomes)
	want := map[string]string{
		"TMS-004": audit.Fail,    // AutoAdmittedUsers differs
		"TMS-005": audit.Pass,    //
		"TMS-009": audit.NotRead, // its Exchange half belongs to the defender step
		"TMS-012": audit.Fail,    // GlobalCatalogAppsType differs; the app lists are not compared
		"SPO-001": audit.Fail,
		"SPO-002": audit.NotRead, // which sites may share is the company's decision
		"SPO-003": audit.Pass,
		"SPO-004": audit.Fail, // a site allows anyone links
		"SPO-007": audit.Fail, // restriction mode differs; the domain list is not compared
		"SPO-008": audit.Fail, // held from writing, but an audit reports it
		"SPO-009": audit.Pass,
		"SPO-013": audit.Fail, // the tenant limits differ; library versions need the site's content
		"SPO-016": audit.NotRead,
		"SPO-018": audit.NotRead,
	}
	for id, w := range want {
		if r := reads[id]; r == nil || r.Status != w {
			t.Errorf("%s = %+v, want %s", id, r, w)
		}
	}
	if d := reads["SPO-016"].Detail; !strings.Contains(d, "Global Reader cannot open") {
		t.Errorf("SPO-016 detail %q", d)
	}
	if d := reads["TMS-004"].Detail; !strings.Contains(d, "AutoAdmittedUsers") {
		t.Errorf("TMS-004 detail %q", d)
	}
	// With TMS-012's lists not compared, only the type is a difference.
	if d := reads["TMS-012"].Detail; strings.Contains(d, "GlobalCatalogApps:") || !strings.Contains(d, "GlobalCatalogAppsType") {
		t.Errorf("TMS-012 detail %q", d)
	}
}
