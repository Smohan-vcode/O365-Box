package choices

import (
	"strings"
	"testing"
)

const dialFixture = `dial:
  question: How careful?
  levels:
    - {value: essential, label: Essential, detail: Fewest changes., tier: core, pick: essential}
    - {value: recommended, label: Recommended, detail: For most., tier: core, pick: recommended, recommended: true}
    - {value: strict, label: Strict, detail: Strictest., tier: enhanced, pick: strict}
`

func TestDialAnswersSetTierAndDefaults(t *testing.T) {
	text := dialFixture + strings.Replace(fixture, "  - id: c.store\n", "  - id: c.store\n    strict: intune\n    essential: open\n", 1)
	text = strings.Replace(text, "  - id: b.forms\n", "  - id: b.forms\n    strict: \"off\"\n", 1)
	c := load(t, text)
	if issues := c.Validate(known()); len(issues) != 0 {
		t.Fatalf("issues: %v", issues)
	}
	ctx := Context{Modules: map[string]bool{"devices": true, "sales": true}}
	cases := []struct {
		level, tier, forms, store string
	}{
		{"essential", "core", "on", "open"},     // forms keeps its sales default; store takes essential
		{"recommended", "core", "on", "intune"}, // every choice on its default
		{"strict", "enhanced", "off", "intune"}, // stricter option where the choice names one
	}
	for _, tc := range cases {
		tier, ans, err := c.DialAnswers(tc.level, ctx)
		if err != nil {
			t.Fatal(err)
		}
		if tier != tc.tier || ans["b.forms"] != tc.forms || ans["c.store"] != tc.store || ans["a.mode"] != "none" {
			t.Errorf("%s: tier %s answers %v", tc.level, tier, ans)
		}
	}
	_, ans, _ := c.DialAnswers("strict", Context{Modules: map[string]bool{}})
	if _, ok := ans["c.store"]; ok {
		t.Error("a choice that does not apply got an answer")
	}
	if _, _, err := c.DialAnswers("reckless", ctx); err == nil {
		t.Error("unknown level accepted")
	}
	if r := c.Dial.Recommended(); r == nil || r.Value != "recommended" {
		t.Errorf("recommended level %v", r)
	}
}

func TestValidateDial(t *testing.T) {
	bad := strings.Replace(dialFixture, "tier: enhanced", "tier: maximum", 1)
	bad = strings.Replace(bad, ", recommended: true", "", 1)
	bad += strings.Replace(fixture, "  - id: c.store\n", "  - id: c.store\n    strict: locked\n", 1)
	var msgs []string
	for _, i := range load(t, bad).Validate(known()) {
		msgs = append(msgs, i.Path+": "+i.Message)
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{
		`choices.c.store.strict: strict names no option "locked"`,
		`dial: level strict: tier "maximum" is not core or enhanced`,
		"dial: exactly one level must be recommended (found 0)",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in\n%s", want, all)
		}
	}
}

// The shipped catalogue: the dial is present, Strict raises the tier and
// picks the stricter options, Recommended keeps every default.
func TestShippedDial(t *testing.T) {
	c, err := Load("../../../defaults/choices.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ctx := Context{Modules: map[string]bool{"devices": true}}
	tier, strict, err := c.DialAnswers("strict", ctx)
	if err != nil || tier != "enhanced" || strict["signin.device_code"] != "block" || strict["devices.apple_android_minimum"] != "latest" {
		t.Errorf("strict: %s %v %v", tier, strict, err)
	}
	tier, rec, _ := c.DialAnswers("recommended", ctx)
	for id, v := range rec {
		if v != c.Choice(id).Default(ctx) || tier != "core" {
			t.Errorf("recommended %s=%s is not the default (tier %s)", id, v, tier)
		}
	}
	tier, ess, _ := c.DialAnswers("essential", ctx)
	if tier != "core" || ess["session.idle_timeout"] != "unmanaged_only" {
		t.Errorf("essential: %s %v", tier, ess)
	}
}
