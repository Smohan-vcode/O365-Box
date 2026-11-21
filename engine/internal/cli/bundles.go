package cli

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Smohan-vcode/O365-Box/engine/internal/bootstrap"
	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
)

// providerModules are providers that only run with a module on.
var providerModules = map[string]string{"intune": "devices"}

func grantData(root string) (*bootstrap.Bundles, map[string]policy.Mapping, error) {
	b, err := bootstrap.LoadBundles(filepath.Join(root, "expected-state", "app-permissions.yaml"))
	if err != nil {
		return nil, nil, err
	}
	maps, _, err := policy.LoadMappings(filepath.Join(root, "expected-state", "setting-ids"))
	if err != nil {
		return nil, nil, err
	}
	return b, maps, nil
}

// mappingKeys are a mapping's grants in bundle key form.
func mappingKeys(m policy.Mapping) []string {
	out := append([]string(nil), m.Permissions...)
	for _, r := range m.Roles {
		out = append(out, "role:"+r)
	}
	return out
}

// moduleOfMapping is the module a setting needs through its provider or a
// bundle it uses, or "".
func moduleOfMapping(b *bootstrap.Bundles, m policy.Mapping) string {
	if mod := providerModules[m.Provider]; mod != "" {
		return mod
	}
	for _, k := range mappingKeys(m) {
		if o := b.Owner(k); o != nil && o.Module != "" {
			return o.Module
		}
	}
	return ""
}

// BundleNeed is one bundle the configuration needs and why (Review screen data).
type BundleNeed struct {
	bootstrap.Bundle
	Reasons  []string `json:"reasons"`
	Settings []string `json:"settings"`
}

// Grants is what bootstrap grants and box check identity expects.
type Grants struct {
	Needs       []BundleNeed          `json:"bundles"`
	Permissions bootstrap.Permissions `json:"permissions"`
}

// IDs are the needed bundle ids.
func (g *Grants) IDs() []string {
	var out []string
	for _, n := range g.Needs {
		out = append(out, n.ID)
	}
	return out
}

// neededGrants computes base plus the bundles the configuration's modules,
// choices and applied settings need (SPEC §24.3).
func neededGrants(root string, cfg *config.Config) (*Grants, error) {
	bundles, maps, err := grantData(root)
	if err != nil {
		return nil, err
	}
	cat, issues, err := loadCatalogue(root)
	if err != nil {
		return nil, err
	}
	if len(issues) > 0 {
		return nil, fmt.Errorf("policy catalogue has %d problem(s); run box policy validate", len(issues))
	}
	plans, err := enginePlans(root)
	if err != nil {
		return nil, err
	}
	caps := map[string]bool{}
	for _, c := range plans[cfg.Tenant.DefaultPlan] {
		caps[c] = true
	}
	sel, err := selection(root, cfg)
	if err != nil {
		return nil, err
	}
	res, err := cat.Resolve(sel, cfg.Tenant.DefaultPlan, caps)
	if err != nil {
		return nil, err
	}
	need := map[string]*BundleNeed{}
	add := func(id, reason, setting string) error {
		b := bundles.Bundle(id)
		if b == nil {
			return fmt.Errorf("unknown permission bundle %q (%s)", id, reason)
		}
		n := need[id]
		if n == nil {
			n = &BundleNeed{Bundle: *b, Reasons: []string{}, Settings: []string{}}
			need[id] = n
		}
		if reason != "" && !slices.Contains(n.Reasons, reason) {
			n.Reasons = append(n.Reasons, reason)
		}
		if setting != "" {
			n.Settings = append(n.Settings, setting)
		}
		return nil
	}
	if err := add(bootstrap.BaseBundle, "always", ""); err != nil {
		return nil, err
	}
	mods := cfg.Modules.Map()
	for _, b := range bundles.Bundles {
		if b.Module != "" && mods[b.Module] {
			if err := add(b.ID, "module "+b.Module, ""); err != nil {
				return nil, err
			}
		}
	}
	for id, why := range sel.ChoiceBundles {
		if err := add(id, why, ""); err != nil {
			return nil, err
		}
	}
	for _, c := range res.Applied {
		for _, s := range c.Settings {
			m := maps[s.ID]
			if m.Status != "resolved" {
				continue // a gate's roles are the person's, not the app's
			}
			for _, k := range mappingKeys(m) {
				if o := bundles.Owner(k); o != nil {
					if o.Module != "" && !mods[o.Module] {
						continue // left out above; never reached
					}
					if err := add(o.ID, "settings", s.ID); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	g := &Grants{}
	for _, id := range sortedKeys(need) {
		n := need[id]
		sort.Strings(n.Settings)
		n.Settings = dedupe(n.Settings)
		g.Needs = append(g.Needs, *n)
	}
	sort.SliceStable(g.Needs, func(a, b int) bool {
		return g.Needs[a].ID == bootstrap.BaseBundle && g.Needs[b].ID != bootstrap.BaseBundle
	})
	g.Permissions, err = bundles.Union(g.IDs())
	return g, err
}

func dedupe(s []string) []string {
	var out []string
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// describe is one line per bundle for the sign-in and the Review screen.
func (n BundleNeed) describe() string {
	why := strings.Join(n.Reasons, "; ")
	if len(n.Settings) > 0 {
		s := n.Settings
		more := ""
		if len(s) > 6 {
			s, more = s[:6], fmt.Sprintf(" and %d more", len(n.Settings)-6)
		}
		why = strings.Replace(why, "settings", fmt.Sprintf("%d setting(s): %s%s", len(n.Settings), strings.Join(s, ", "), more), 1)
	}
	hp := ""
	if n.HighPrivilege {
		hp = " [high privilege]"
	}
	return fmt.Sprintf("%-14s%s %s (needed for: %s)", n.ID, hp, n.Why, why)
}
