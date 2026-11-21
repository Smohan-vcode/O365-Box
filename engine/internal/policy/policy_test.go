package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
)

const (
	catalogueDir = "../../../policy"
	capsFile     = "../../capabilities.yaml"
	exampleFile  = "../../../examples/acme/box-config.yaml"
)

func loadCatalogue(t *testing.T) *Catalogue {
	t.Helper()
	c, err := Load(catalogueDir, "policy")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func plans(t *testing.T) map[string][]string {
	t.Helper()
	b, err := os.ReadFile(capsFile)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Plans map[string][]string `yaml:"plans"`
	}
	if err := yaml.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f.Plans
}

func caps(t *testing.T, plan string) map[string]bool {
	t.Helper()
	list, ok := plans(t)[plan]
	if !ok {
		t.Fatalf("no plan %s", plan)
	}
	m := map[string]bool{}
	for _, c := range list {
		m[c] = true
	}
	return m
}

// exampleSelection is the policy section of examples/acme.
func exampleSelection(t *testing.T) Selection {
	t.Helper()
	d, err := config.Load(exampleFile)
	if err != nil {
		t.Fatal(err)
	}
	p := d.Config.Policy
	sel := Selection{Frameworks: p.Frameworks, Tier: p.Tier, Stage: p.Rollout.Stage}
	for _, e := range p.Exceptions {
		sel.Exceptions = append(sel.Exceptions, Exception{ID: e.ID, Reason: e.Reason, Reference: e.Reference, Ends: e.Ends})
	}
	return sel
}

func TestCatalogueIsValid(t *testing.T) {
	c := loadCatalogue(t)
	for _, i := range c.Validate(plans(t)) {
		t.Error(i)
	}
	if len(c.Requirements) != 90 || c.SettingCount() != 336 || len(c.Centres) != 12 || len(c.Frameworks) != 5 {
		t.Errorf("got %d requirements, %d settings, %d centres, %d frameworks",
			len(c.Requirements), c.SettingCount(), len(c.Centres), len(c.Frameworks))
	}
}

func TestBrokenCatalogueFails(t *testing.T) {
	c, err := Load("testdata/broken", "broken")
	if err != nil {
		t.Fatal(err)
	}
	issues := c.Validate(map[string][]string{"plan_a": {"free"}, "plan_b": {"free"}})
	want := []string{
		"setting SET-002 refers to unknown requirement ZZZ-99",
		"technical requirement AAA-02 has no setting",
		"control C1 refers to unknown requirement YYY-42",
		`duplicate setting id "SET-001"`,
		`mode "sometimes"`,
		`unknown capability "paid"`,
		"requirement AAA-02: fw C9 is not a control",
		"gives plan plan_b capability free, but included_in does not list it",
	}
	text := ""
	for _, i := range issues {
		text += i.String() + "\n"
		if i.File == "" {
			t.Errorf("issue without a file: %v", i)
		}
	}
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("want %q in:\n%s", w, text)
		}
	}
	if !strings.Contains(text, "broken/settings/centre.yaml:11") {
		t.Errorf("want file and line of SET-002 in:\n%s", text)
	}
}

func TestResolveMatchesReferenceCoverage(t *testing.T) {
	// policy/coverage.md of the source package: plan business_premium,
	// tier core (called baseline there), stage audit, no exceptions.
	c := loadCatalogue(t)
	sel := Selection{Frameworks: []string{"nist_csf_2", "nis2", "iso27001_2022", "soc2", "cis_v8"}, Tier: TierCore, Stage: ModeAudit}
	res, err := c.Resolve(sel, "business_premium", caps(t, "business_premium"))
	if err != nil {
		t.Fatal(err)
	}
	if res.AppliedCount() != 307 || len(res.NotApplied) != 29 {
		t.Errorf("applied %d, not applied %d; want 307 and 29 (305 before T14-19 added WIN-091 and MAC-032, which a choice leaves out by default)", res.AppliedCount(), len(res.NotApplied))
	}
	want := map[string]int{StatusEnforced: 62, StatusEnforcedProcedure: 11, StatusProcedure: 11, StatusAddon: 5, StatusNotSelected: 1}
	for st, n := range want {
		if res.Counts[st] != n {
			t.Errorf("%s: %d, want %d", st, res.Counts[st], n)
		}
	}
	md := res.CoverageMarkdown(c)
	for _, line := range []string{
		"| PR.AA | Identity management, authentication and access control | 30 | 27 | 1 | 2 |",
		"| g | Basic cyber hygiene practices and cybersecurity training | 32 | 29 | 2 | 0 |",
		"| CC6.7 |  | 20 | 18 | 0 | 1 |",
		"No requirement in this catalogue addresses: RC.CO.",
		"| BCK-02 | Mail and files can be restored from a backup held independently of the live service. |",
	} {
		if !strings.Contains(md, line) {
			t.Errorf("coverage lacks %q", line)
		}
	}
}

func TestResolveExample(t *testing.T) {
	c := loadCatalogue(t)
	sel := exampleSelection(t)
	res, err := c.Resolve(sel, "business_premium", caps(t, "business_premium"))
	if err != nil {
		t.Fatal(err)
	}
	if res.AppliedCount() == 0 || len(res.Applied) != 12 {
		t.Fatalf("applied %d in %d centres", res.AppliedCount(), len(res.Applied))
	}
	reasons := map[string]string{}
	for _, n := range res.NotApplied {
		reasons[n.ID] = n.Reason
	}
	for id, want := range map[string]string{
		"DEF-022": "needs Defender Suite for Business Premium",
		"ENT-014": "tier enhanced not selected",
		"TMS-005": "exception CHG-0001",
	} {
		if reasons[id] != want {
			t.Errorf("%s: reason %q, want %q", id, reasons[id], want)
		}
	}
	addon := 0
	for _, ce := range c.Centres {
		for _, s := range ce.Settings {
			if s.Tier == TierAddon {
				addon++
				if _, ok := reasons[s.ID]; !ok {
					t.Errorf("addon setting %s applied", s.ID)
				}
			}
		}
	}
	if addon == 0 {
		t.Error("catalogue has no addon settings")
	}
	if m := mode(res, "ENT-001"); m != ModeAudit {
		t.Errorf("staged ENT-001 in stage audit: mode %q", m)
	}
	if m := mode(res, "TMS-001"); m != ModeEnforce {
		t.Errorf("TMS-001: mode %q", m)
	}
	if len(res.Coverage) != 2 || res.Coverage[0].ID != "soc2" || res.Coverage[1].ID != "nist_csf_2" {
		t.Errorf("coverage frameworks: %+v", res.Coverage)
	}

	sel.Stage = ModeEnforce
	enf, err := c.Resolve(sel, "business_premium", caps(t, "business_premium"))
	if err != nil {
		t.Fatal(err)
	}
	if m := mode(enf, "ENT-001"); m != ModeEnforce {
		t.Errorf("staged ENT-001 in stage enforce: mode %q", m)
	}

	sel.Tier = TierEnhanced
	more, err := c.Resolve(sel, "business_premium", caps(t, "business_premium"))
	if err != nil {
		t.Fatal(err)
	}
	if more.AppliedCount() <= res.AppliedCount() || mode(more, "ENT-014") == "" {
		t.Errorf("enhanced applied %d, core %d", more.AppliedCount(), res.AppliedCount())
	}

	basic, err := c.Resolve(exampleSelection(t), "business_basic", caps(t, "business_basic"))
	if err != nil {
		t.Fatal(err)
	}
	if mode(basic, "ENT-001") != "" || basic.AppliedCount() >= res.AppliedCount() {
		t.Error("business_basic must not apply Conditional Access")
	}
}

func TestRequirementException(t *testing.T) {
	c := loadCatalogue(t)
	sel := Selection{Frameworks: []string{"soc2"}, Tier: TierCore, Stage: ModeAudit,
		Exceptions: []Exception{{ID: "COL-03", Reason: "test", Reference: "CHG-1", Ends: "2099-01-01"}}}
	res, err := c.Resolve(sel, "business_premium", caps(t, "business_premium"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status["COL-03"] != StatusException {
		t.Errorf("COL-03: %s", res.Status["COL-03"])
	}
	for _, s := range c.Requirement("COL-03").Settings {
		only := len(c.Setting(s).Requirements) == 1
		if got := mode(res, s) != ""; got == only {
			t.Errorf("%s applied=%v; serves only COL-03=%v", s, got, only)
		}
	}
	if _, err := c.Resolve(Selection{Tier: TierCore, Stage: ModeAudit, Exceptions: []Exception{{ID: "NOPE-1"}}}, "x", nil); err == nil {
		t.Error("want error for an unknown exception id")
	}
}

func TestWriteDir(t *testing.T) {
	c := loadCatalogue(t)
	res, err := c.Resolve(exampleSelection(t), "business_premium", caps(t, "business_premium"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	names, err := res.WriteDir(c, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 14 {
		t.Errorf("wrote %v", names)
	}
	var out struct {
		AdminCentre string           `yaml:"admin_centre"`
		Settings    []AppliedSetting `yaml:"settings"`
	}
	b, err := os.ReadFile(filepath.Join(dir, "entra.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.AdminCentre != "Microsoft Entra" || len(out.Settings) == 0 || out.Settings[0].Mode == "" {
		t.Errorf("entra.yaml: %+v", out)
	}
	b, err = os.ReadFile(filepath.Join(dir, "not-applied.yaml"))
	if err != nil || !strings.Contains(string(b), "TMS-005") {
		t.Errorf("not-applied.yaml lacks TMS-005: %v", err)
	}
}

func mode(r *Result, id string) string {
	for _, c := range r.Applied {
		for _, s := range c.Settings {
			if s.ID == id {
				return s.Mode
			}
		}
	}
	return ""
}
