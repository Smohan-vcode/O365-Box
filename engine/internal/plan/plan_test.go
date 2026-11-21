package plan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeCatalog(t *testing.T, manifests map[string]string, gates string) *Catalog {
	t.Helper()
	dir := t.TempDir()
	for name, body := range manifests {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "steps.yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gf := ""
	if gates != "" {
		gf = filepath.Join(dir, "gates.yaml")
		if err := os.WriteFile(gf, []byte(gates), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c, err := Load(dir, gf)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const baseManifests = `provider: a
steps:
  - {id: a.one}
  - {id: a.two, depends_on: [a.one]}
  - {id: a.ca, depends_on: [a.one], capabilities: [conditional_access], fallback: a.defaults}
  - {id: a.defaults, depends_on: [a.one], fallback_only: true}
  - {id: a.intune, capabilities: [intune], module: devices}
  - {id: a.after-ca, depends_on: [a.ca]}
  - {id: a.sales, module: sales, depends_on: [a.two]}
  - {id: a.gated, gate: G-01, depends_on: [a.two]}
  - {id: a.check, check_only: true}
`
const baseGates = "gates:\n  - {id: G-01, title: Licences, check: a.check}\n"

func TestBuildOrderModulesAndFallbacks(t *testing.T) {
	c := writeCatalog(t, map[string]string{"a": baseManifests}, baseGates)
	g, err := Build(c, Options{Modules: map[string]bool{"devices": true}, Capabilities: map[string]bool{}})
	if err != nil {
		t.Fatal(err)
	}
	if g.Nodes["a.sales"] != nil {
		t.Error("sales step present with module off")
	}
	if g.Nodes["a.ca"] != nil || g.Nodes["a.defaults"] == nil || g.Nodes["a.defaults"].Replaces != "a.ca" {
		t.Error("fallback not substituted")
	}
	if !reflect.DeepEqual(g.Nodes["a.after-ca"].DependsOn, []string{"a.defaults"}) {
		t.Errorf("dependency not redirected to fallback: %v", g.Nodes["a.after-ca"].DependsOn)
	}
	if !strings.Contains(g.Nodes["a.intune"].Skip, "needs intune") {
		t.Errorf("missing capability not marked: %q", g.Nodes["a.intune"].Skip)
	}
	pos := map[string]int{}
	for i, id := range g.Order {
		pos[id] = i
	}
	for id, n := range g.Nodes {
		for _, d := range n.DependsOn {
			if pos[d] > pos[id] {
				t.Errorf("%s ordered before its dependency %s", id, d)
			}
		}
	}
	g2, _ := Build(c, Options{Modules: map[string]bool{"devices": true}, Capabilities: map[string]bool{"conditional_access": true}})
	if g2.Nodes["a.ca"] == nil || g2.Nodes["a.defaults"] != nil {
		t.Error("with the capability, the original step must run and the fallback must not")
	}
}

func TestBuildOnly(t *testing.T) {
	c := writeCatalog(t, map[string]string{"a": baseManifests}, baseGates)
	g, err := Build(c, Options{Only: []string{"a.two"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.Order, []string{"a.one", "a.two"}) {
		t.Errorf("order = %v", g.Order)
	}
	if _, err := Build(c, Options{Only: []string{"a.nope"}}); err == nil {
		t.Error("want error for unknown --only step")
	}
	g, err = Build(c, Options{Only: []string{"a.ca"}})
	if err != nil || g.Nodes["a.defaults"] == nil {
		t.Errorf("--only on a replaced step should select its fallback: %v %v", g, err)
	}
}

func TestBuildErrors(t *testing.T) {
	cases := map[string]struct{ manifest, gates, want string }{
		"unknown dependency":   {"provider: a\nsteps:\n  - {id: a.x, depends_on: [a.nope]}\n", "", "unknown step a.nope"},
		"cycle":                {"provider: a\nsteps:\n  - {id: a.x, depends_on: [a.y]}\n  - {id: a.y, depends_on: [a.x]}\n", "", "cycle"},
		"unknown gate":         {"provider: a\nsteps:\n  - {id: a.x, gate: G-9}\n", "", "unknown gate G-9"},
		"unknown fallback":     {"provider: a\nsteps:\n  - {id: a.x, fallback: a.nope}\n", "", "unknown fallback"},
		"disabled module":      {"provider: a\nsteps:\n  - {id: a.x, module: sales}\n  - {id: a.y, depends_on: [a.x]}\n", "", "module sales is off"},
		"bad gate check":       {"provider: a\nsteps:\n  - {id: a.x}\n", "gates:\n  - {id: G-1, check: a.nope}\n", "unknown check step"},
		"check not check_only": {"provider: a\nsteps:\n  - {id: a.x}\n", "gates:\n  - {id: G-1, check: a.x}\n", "must set check_only"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cat := writeCatalog(t, map[string]string{"a": c.manifest}, c.gates)
			_, err := Build(cat, Options{})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestLoadRejects(t *testing.T) {
	dir := t.TempDir()
	for _, m := range []struct{ name, body, want string }{
		{"two-modules", "provider: x\nmodules: [A, B]\nsteps: []\n", "at most one"},
		{"no-provider", "steps: []\n", "provider is required"},
	} {
		d := filepath.Join(dir, m.name)
		_ = os.MkdirAll(filepath.Join(d, "p"), 0o755)
		_ = os.WriteFile(filepath.Join(d, "p", "steps.yaml"), []byte(m.body), 0o644)
		if _, err := Load(d, ""); err == nil || !strings.Contains(err.Error(), m.want) {
			t.Errorf("%s: err = %v", m.name, err)
		}
	}
}
