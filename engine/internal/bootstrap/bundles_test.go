package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryBundles(t *testing.T) {
	b, err := LoadBundles("../../../expected-state/app-permissions.yaml")
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.Union(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range p.Graph {
		if strings.HasPrefix(g, "DeviceManagement") {
			t.Errorf("base grants device permission %s", g)
		}
	}
	for _, r := range append(p.EntraRoles, b.Bundle("devices").EntraRoles...) {
		if _, ok := RoleTemplates[r]; !ok {
			t.Errorf("role %s has no template id", r)
		}
	}
	for _, bd := range b.Bundles {
		for _, r := range bd.EntraRoles {
			if _, ok := RoleTemplates[r]; !ok {
				t.Errorf("bundle %s: role %s has no template id", bd.ID, r)
			}
		}
	}
	if _, err := b.Union([]string{"nope"}); err == nil {
		t.Error("unknown bundle should fail")
	}
}

func TestLoadBundlesRejects(t *testing.T) {
	for name, text := range map[string]string{
		"no base":   "bundles:\n  - id: x\n    graph: [A]\n",
		"duplicate": "bundles:\n  - id: base\n    graph: [A]\n  - id: x\n    graph: [A]\n",
		"never":     "bundles:\n  - id: base\n    graph: [Mail.Send]\nnever: [Mail.Send]\n",
	} {
		p := filepath.Join(t.TempDir(), "p.yaml")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadBundles(p); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
