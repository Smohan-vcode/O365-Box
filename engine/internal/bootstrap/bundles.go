package bootstrap

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// Bundle is one group of the pipeline app's permissions (SPEC §24.3).
type Bundle struct {
	ID            string   `yaml:"id" json:"id"`
	Module        string   `yaml:"module" json:"module,omitempty"`
	Why           string   `yaml:"why" json:"why"`
	HighPrivilege bool     `yaml:"high_privilege" json:"high_privilege,omitempty"`
	Graph         []string `yaml:"graph" json:"graph,omitempty"`
	Exchange      []string `yaml:"exchange" json:"exchange,omitempty"`
	SharePoint    []string `yaml:"sharepoint" json:"sharepoint,omitempty"`
	EntraRoles    []string `yaml:"entra_roles" json:"entra_roles,omitempty"`
}

// Bundles is expected-state/app-permissions.yaml.
type Bundles struct {
	Bundles []Bundle `yaml:"bundles"`
	Never   []string `yaml:"never"`
}

// BaseBundle is always granted.
const BaseBundle = "base"

// LoadBundles reads and checks the file: unique ids, a base bundle, no
// permission in two bundles and none that is never allowed.
func LoadBundles(path string) (*Bundles, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f Bundles
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	ids := map[string]bool{}
	owner := map[string]string{}
	never := map[string]bool{}
	for _, n := range f.Never {
		never[n] = true
	}
	for _, bd := range f.Bundles {
		if bd.ID == "" || ids[bd.ID] {
			return nil, fmt.Errorf("%s: missing or duplicate bundle id %q", path, bd.ID)
		}
		ids[bd.ID] = true
		for _, k := range bd.Keys() {
			if prev, dup := owner[k]; dup {
				return nil, fmt.Errorf("%s: %s is in bundles %s and %s", path, k, prev, bd.ID)
			}
			owner[k] = bd.ID
			if never[lastPart(k)] {
				return nil, fmt.Errorf("%s: bundle %s holds %s, which is never allowed", path, bd.ID, k)
			}
		}
	}
	if !ids[BaseBundle] {
		return nil, fmt.Errorf("%s: no %q bundle", path, BaseBundle)
	}
	return &f, nil
}

func lastPart(k string) string {
	for i := len(k) - 1; i >= 0; i-- {
		if k[i] == ':' {
			return k[i+1:]
		}
	}
	return k
}

// Keys are the bundle's grants as "graph:X", "exchange:X", "sharepoint:X" and "role:X".
func (b Bundle) Keys() []string {
	var out []string
	for _, l := range []struct {
		p string
		v []string
	}{{"graph:", b.Graph}, {"exchange:", b.Exchange}, {"sharepoint:", b.SharePoint}, {"role:", b.EntraRoles}} {
		for _, v := range l.v {
			out = append(out, l.p+v)
		}
	}
	return out
}

// Bundle returns a bundle by id, or nil.
func (f *Bundles) Bundle(id string) *Bundle {
	for i := range f.Bundles {
		if f.Bundles[i].ID == id {
			return &f.Bundles[i]
		}
	}
	return nil
}

// Owner returns the bundle that holds a grant key, or nil.
func (f *Bundles) Owner(key string) *Bundle {
	for i := range f.Bundles {
		for _, k := range f.Bundles[i].Keys() {
			if k == key {
				return &f.Bundles[i]
			}
		}
	}
	return nil
}

// Union is the permissions of the named bundles (base is always included).
func (f *Bundles) Union(ids []string) (Permissions, error) {
	p := Permissions{Never: append([]string(nil), f.Never...)}
	seen := map[string]bool{}
	for _, id := range append([]string{BaseBundle}, ids...) {
		if seen[id] {
			continue
		}
		seen[id] = true
		b := f.Bundle(id)
		if b == nil {
			return p, fmt.Errorf("unknown permission bundle %q", id)
		}
		p.Graph = append(p.Graph, b.Graph...)
		p.Exchange = append(p.Exchange, b.Exchange...)
		p.SharePoint = append(p.SharePoint, b.SharePoint...)
		p.EntraRoles = append(p.EntraRoles, b.EntraRoles...)
	}
	for _, l := range []*[]string{&p.Graph, &p.Exchange, &p.SharePoint, &p.EntraRoles} {
		sort.Strings(*l)
	}
	return p, nil
}
