package choices

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = `choices:
  - id: a.mode
    step: structure
    question: Which?
    detect: a.fact
    options:
      - value: existing
        label: Existing
        when: detected
        recommended: true
        cost: none
        effects: {modules: [azure], bundles: [azure]}
      - value: none
        label: None
        recommended_when: not detected
        cost: none
  - id: b.forms
    step: security
    question: Forms?
    options:
      - value: "on"
        label: On
        recommended_when: module sales
        cost: none
        effects: {exceptions: [ADM-005]}
      - value: "off"
        label: Off
        recommended: true
        cost: none
  - id: c.store
    step: security
    question: Store?
    when: module devices
    options:
      - value: intune
        label: Intune
        recommended: true
        cost: none
        effects: {variants: {WIN-058: intune}}
      - value: open
        label: Open
        cost: none
        effects: {exceptions: [WIN-058]}
`

func load(t *testing.T, text string) *Catalogue {
	t.Helper()
	p := filepath.Join(t.TempDir(), "choices.yaml")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func known() Known {
	return Known{
		Settings: map[string]bool{"ADM-005": true, "WIN-058": true},
		Variants: map[string]map[string]bool{"WIN-058": {"intune": true}},
		Modules:  map[string]bool{"azure": true, "devices": true, "sales": true},
		Bundles:  map[string]bool{"azure": true},
		Gates:    map[string]bool{},
	}
}

func TestValidateFixture(t *testing.T) {
	if issues := load(t, fixture).Validate(known()); len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
}

func TestValidateNamesBadReferences(t *testing.T) {
	bad := strings.Replace(fixture, "WIN-058: intune", "WIN-058: private", 1)
	bad = strings.Replace(bad, "bundles: [azure]", "bundles: [cloud]", 1)
	bad = strings.Replace(bad, "exceptions: [ADM-005]", "exceptions: [ADM-999]", 1)
	var msgs []string
	for _, i := range load(t, bad).Validate(known()) {
		msgs = append(msgs, i.Path+": "+i.Message)
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{
		`choices.c.store.intune: setting WIN-058 has no variant "private"`,
		"choices.a.mode.existing: unknown permission bundle cloud",
		"choices.b.forms.on: unknown setting ADM-999",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in\n%s", want, all)
		}
	}
}

func TestResolveDefaultsAndAnswers(t *testing.T) {
	c := load(t, fixture)
	r, err := c.Resolve(nil, Context{Modules: map[string]bool{}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Answers["a.mode"] != "none" || r.Answers["b.forms"] != "off" || len(r.LeftOut) != 0 || len(r.Bundles) != 0 {
		t.Errorf("defaults without facts: %+v", r)
	}
	if _, asked := r.Answers["c.store"]; asked {
		t.Error("c.store is only asked with the devices module")
	}

	r, _ = c.Resolve(nil, Context{Modules: map[string]bool{"sales": true, "devices": true}, Detected: map[string]bool{"a.fact": true}})
	if r.Answers["a.mode"] != "existing" || r.Bundles["azure"] == "" || r.LeftOut["ADM-005"] != "choice b.forms=on" || r.Variants["WIN-058"] != "intune" {
		t.Errorf("defaults with sales, devices and the fact: %+v", r)
	}

	r, _ = c.Resolve(map[string]string{"c.store": "open"}, Context{Modules: map[string]bool{"devices": true}})
	if r.Defaulted["c.store"] || r.LeftOut["WIN-058"] != "choice c.store=open" || r.Variants["WIN-058"] != "" {
		t.Errorf("answered: %+v", r)
	}

	if _, err := c.Resolve(map[string]string{"b.forms": "maybe"}, Context{}); err == nil || !strings.Contains(err.Error(), `no option "maybe"`) {
		t.Errorf("unknown option: %v", err)
	}
	if _, err := c.Resolve(map[string]string{"z.none": "x"}, Context{}); err == nil {
		t.Error("unknown choice should fail")
	}
}
