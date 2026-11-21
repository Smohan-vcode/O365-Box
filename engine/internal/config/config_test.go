package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/report"
)

const examplePath = "../../../examples/acme/box-config.yaml"

var testRoles = []string{"owner", "legal-approver", "company-legal-admin", "signer", "sender", "finance-approver", "control-owner"}

func loadExample(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func validateText(t *testing.T, text string) []report.Issue {
	t.Helper()
	d, err := Parse("fixture.yaml", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	issues, err := Validate(d, Options{Roles: testRoles})
	if err != nil {
		t.Fatal(err)
	}
	return issues
}

func TestExampleIsValid(t *testing.T) {
	if issues := validateText(t, loadExample(t)); len(issues) > 0 {
		for _, i := range issues {
			t.Error(i)
		}
	}
}

func TestInvalidConfigurations(t *testing.T) {
	cases := []struct {
		name, old, new, path, want string
	}{
		{"flow owner unknown", "flow_owner: founder", "flow_owner: nobody", "tenant.flow_owner", `flow owner "nobody" is not a user`},
		{"schema pattern", "usage_location: GB", "usage_location: gb", "tenant.usage_location", "does not match pattern"},
		{"secret must be a reference", "dns_token: keychain:", "dns_token: plain-", "secrets.dns_token", "does not match pattern"},
		{"library loosens without reason", "      access_reason: Sales reads approved templates\n", "", "structure.sites[1].libraries[0].access", "more access than site"},
		{"individual granted access", "    access:\n      grp-finance: owner\n", "    access:\n      grp-finance: owner\n      robin: member\n", "structure.sites[2].access.robin", "groups only"},
		{"everyone granted access", "    access:\n      grp-finance: owner\n", "    access:\n      grp-finance: owner\n      Everyone: visitor\n", "structure.sites[2].access.Everyone", "never granted"},
		{"external sharing without reason", "    external: \"off\"\n    classification: Confidential\n    access:\n      grp-finance: owner", "    external: new_guests\n    classification: Confidential\n    access:\n      grp-finance: owner", "structure.sites[2].external", "needs external_reason"},
		{"admin site removed", "- name: Admin\n", "- name: Office\n", "structure.sites", "Admin site cannot be removed"},
		{"unknown role", "  - control-owner\n", "  - chief-wizard\n", "users[0].roles[6]", `role "chief-wizard" is not in defaults/roles.yaml`},
		{"unknown mailbox access group", "  - name: billing\n    access: grp-finance", "  - name: billing\n    access: grp-money", "mail.shared[3].access", `access group "grp-money" is not defined`},
		{"unknown list member", "  - name: dmarc\n    members:\n    - founder", "  - name: dmarc\n    members:\n    - ghost", "mail.lists[1].members[0]", `member "ghost"`},
		{"module dependency", "  records: true", "  records: false", "modules.people", "needs module records"},
		{"registration not operating", "  registered: gb", "  registered: fr", "jurisdictions.operating", "must also be listed as operating"},
		{"break-glass with a plan", "  family_name: One\n  plan: none", "  family_name: One\n  plan: business_basic", "tenant.break_glass_accounts[0]", "must have plan none"},
		{"duplicate address", "  primary: sam", "  primary: robin", "users[1].primary", "already used"},
	}
	base := loadExample(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(base, c.old) {
				t.Fatalf("fixture text not found: %q", c.old)
			}
			issues := validateText(t, strings.Replace(base, c.old, c.new, 1))
			for _, i := range issues {
				if i.Path == c.path && strings.Contains(i.Message, c.want) {
					if i.Line == 0 {
						t.Errorf("issue has no line number: %v", i)
					}
					return
				}
			}
			t.Errorf("want issue at %s containing %q; got:", c.path, c.want)
			for _, i := range issues {
				t.Log("  ", i)
			}
		})
	}
}

func TestEngineDataChecks(t *testing.T) {
	d, err := Parse("fixture.yaml", []byte(loadExample(t)))
	if err != nil {
		t.Fatal(err)
	}
	cat := &PolicyCatalog{Frameworks: []string{"soc2"}, Settings: []string{"ENT-001"}, Requirements: []string{"IAM-01"}}
	issues, err := Validate(d, Options{Roles: testRoles, Policy: cat, Jurisdictions: []string{"gb", "eu"}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"policy.frameworks[1]":       `framework "nist_csf_2" is not in policy/frameworks`,
		"policy.exceptions[0].id":    `"TMS-005" is neither a setting nor a requirement`,
		"jurisdictions.operating[2]": "defaults/jurisdictions/de.yaml",
	}
	for path, msg := range want {
		found := false
		for _, i := range issues {
			found = found || (i.Path == path && strings.Contains(i.Message, msg))
		}
		if !found {
			t.Errorf("want %s: %s; got %v", path, msg, issues)
		}
	}
}

func TestPolicyChecks(t *testing.T) {
	base := loadExample(t)
	day := func(s string) time.Time {
		t.Helper()
		d, err := time.Parse(DateLayout, s)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	cases := []struct {
		name, old, new, today, path, want string
	}{
		{"expired exception", "ends: '2099-12-31'", "ends: '2026-01-31'", "2026-02-01", "policy.exceptions[0].ends", "expired on 2026-01-31"},
		{"exception ends today is still valid", "ends: '2099-12-31'", "ends: '2026-02-01'", "2026-02-01", "", ""},
		{"exception end not a date", "ends: '2099-12-31'", "ends: '2099-02-31'", "2026-02-01", "policy.exceptions[0].ends", "not a date"},
		{"exception end pattern", "ends: '2099-12-31'", "ends: 31/12/2099", "2026-02-01", "policy.exceptions[0].ends", "does not match pattern"},
		{"pilot group undefined", "pilot_group: grp-admins", "pilot_group: grp-pilots", "2026-02-01", "policy.rollout.pilot_group", `pilot group "grp-pilots" is not defined`},
		{"pilot group not an access group", "pilot_group: grp-admins", "pilot_group: grp-break-glass", "2026-02-01", "policy.rollout.pilot_group", "not an access group"},
		{"unknown tier", "tier: core", "tier: baseline", "2026-02-01", "policy.tier", "value must be one of"},
		{"policy required", "policy:\n  frameworks:", "unused:\n  frameworks:", "2026-02-01", "", "missing property 'policy'"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(base, c.old) {
				t.Fatalf("fixture text not found: %q", c.old)
			}
			d, err := Parse("fixture.yaml", []byte(strings.Replace(base, c.old, c.new, 1)))
			if err != nil {
				t.Fatal(err)
			}
			issues, err := Validate(d, Options{Roles: testRoles, Today: day(c.today)})
			if err != nil {
				t.Fatal(err)
			}
			if c.want == "" {
				for _, i := range issues {
					t.Error(i)
				}
				return
			}
			for _, i := range issues {
				if i.Path == c.path && strings.Contains(i.Message, c.want) {
					return
				}
			}
			t.Errorf("want issue at %s containing %q; got:", c.path, c.want)
			for _, i := range issues {
				t.Log("  ", i)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	d, err := Parse("x.yaml", []byte("a:\n  b: ''\n  list:\n  - {n: one}\n  - {n: ''}\nempty: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path      string
		wantPaths []string
		empty     []bool
	}{
		{"a.b", []string{"a.b"}, []bool{true}},
		{"a.list[*].n", []string{"a.list[0].n", "a.list[1].n"}, []bool{false, true}},
		{"a.list[1].n", []string{"a.list[1].n"}, []bool{true}},
		{"empty[*].x", []string{"empty[*].x"}, []bool{true}},
		{"missing.key", []string{"missing.key"}, []bool{true}},
	}
	for _, c := range cases {
		vals, err := Resolve(d.Raw, c.path)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		if len(vals) != len(c.wantPaths) {
			t.Fatalf("%s: got %d values, want %d", c.path, len(vals), len(c.wantPaths))
		}
		for i, v := range vals {
			if v.Path != c.wantPaths[i] || IsEmpty(v.Value) != c.empty[i] {
				t.Errorf("%s[%d]: got %s empty=%v", c.path, i, v.Path, IsEmpty(v.Value))
			}
		}
	}
	if _, err := Resolve(d.Raw, "a..b"); err == nil {
		t.Error("want error for a..b")
	}
	if _, err := Resolve(d.Raw, "a[x]"); err == nil {
		t.Error("want error for a[x]")
	}
}

func TestLineNumbers(t *testing.T) {
	d, err := Parse(filepath.Base(examplePath), []byte(loadExample(t)))
	if err != nil {
		t.Fatal(err)
	}
	if d.Line("tenant.flow_owner") == 0 || d.Line("structure.sites[8].libraries[0].name") == 0 {
		t.Error("missing line numbers")
	}
	if d.Line("tenant.no_such_key") != d.Line("tenant") {
		t.Error("unknown path should fall back to its parent's line")
	}
}
