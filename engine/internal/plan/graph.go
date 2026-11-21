package plan

import (
	"fmt"
	"sort"
	"strings"
)

// Node is a step in a built graph.
type Node struct {
	Step       *Step
	Replaces   string   // set when this node is a fallback for another step
	Skip       string   // reason the step will not run, if any
	DependsOn  []string // resolved dependencies (after fallback substitution)
	Dependents []string
}

// Graph is the ordered set of steps for one configuration.
type Graph struct {
	Nodes map[string]*Node
	Order []string // topological, ties broken by id
}

// Options select the steps for a configuration.
type Options struct {
	Modules      map[string]bool // enabled modules; core is always on
	Capabilities map[string]bool // capabilities the tenant has
	Only         []string        // if set, these steps and their dependencies
	Exact        bool            // with Only: just these steps, not their dependencies
}

// Build resolves modules, capabilities and fallbacks, and orders the steps.
// It fails on unknown dependencies, gates or fallbacks, and on cycles.
func Build(c *Catalog, opt Options) (*Graph, error) {
	g := &Graph{Nodes: map[string]*Node{}}
	enabled := func(s *Step) bool { return s.Module == "core" || opt.Modules[s.Module] }
	missing := func(s *Step) []string {
		var m []string
		for _, cap := range s.Capabilities {
			if !opt.Capabilities[cap] {
				m = append(m, cap)
			}
		}
		return m
	}
	ids := make([]string, 0, len(c.Steps))
	for id := range c.Steps {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	replaced := map[string]string{} // original -> fallback
	for _, id := range ids {
		s := c.Steps[id]
		if s.Gate != "" && c.Gates[s.Gate] == nil {
			return nil, fmt.Errorf("step %s: unknown gate %s", id, s.Gate)
		}
		if s.Fallback != "" && c.Steps[s.Fallback] == nil {
			return nil, fmt.Errorf("step %s: unknown fallback %s", id, s.Fallback)
		}
		for _, d := range s.DependsOn {
			if c.Steps[d] == nil {
				return nil, fmt.Errorf("step %s depends on unknown step %s", id, d)
			}
		}
		if !enabled(s) || s.FallbackOnly || s.CheckOnly {
			continue
		}
		n := &Node{Step: s}
		if m := missing(s); len(m) > 0 {
			if s.Fallback != "" {
				fb := c.Steps[s.Fallback]
				if fm := missing(fb); len(fm) > 0 {
					n.Skip = fmt.Sprintf("needs %s; fallback %s needs %s", strings.Join(m, ", "), fb.ID, strings.Join(fm, ", "))
				} else {
					replaced[id] = fb.ID
					g.Nodes[fb.ID] = &Node{Step: fb, Replaces: id}
					continue
				}
			} else {
				n.Skip = "needs " + strings.Join(m, ", ")
			}
		}
		g.Nodes[id] = n
	}
	for _, gate := range c.Gates {
		if gate.Check == "" {
			continue
		}
		cs := c.Steps[gate.Check]
		if cs == nil {
			return nil, fmt.Errorf("gate %s: unknown check step %s", gate.ID, gate.Check)
		}
		if !cs.CheckOnly {
			return nil, fmt.Errorf("gate %s: check step %s must set check_only", gate.ID, gate.Check)
		}
	}
	// Resolve dependencies: through fallbacks, and drop those in disabled modules.
	for id, n := range g.Nodes {
		for _, d := range n.Step.DependsOn {
			if fb, ok := replaced[d]; ok {
				d = fb
			}
			if g.Nodes[d] == nil {
				dep := c.Steps[d]
				if !enabled(dep) {
					return nil, fmt.Errorf("step %s depends on %s, whose module %s is off", id, d, dep.Module)
				}
				return nil, fmt.Errorf("step %s depends on %s, which is not in the graph", id, d)
			}
			n.DependsOn = append(n.DependsOn, d)
		}
		sort.Strings(n.DependsOn)
	}
	if len(opt.Only) > 0 {
		keep := map[string]bool{}
		var add func(id string) error
		add = func(id string) error {
			if keep[id] {
				return nil
			}
			n := g.Nodes[id]
			if n == nil {
				if fb, ok := replaced[id]; ok {
					return add(fb)
				}
				return fmt.Errorf("step %s is not in the graph for this configuration", id)
			}
			keep[id] = true
			if opt.Exact {
				return nil
			}
			for _, d := range n.DependsOn {
				if err := add(d); err != nil {
					return err
				}
			}
			return nil
		}
		for _, id := range opt.Only {
			if err := add(id); err != nil {
				return nil, err
			}
		}
		for id := range g.Nodes {
			if !keep[id] {
				delete(g.Nodes, id)
			}
		}
		for _, n := range g.Nodes {
			var deps []string
			for _, d := range n.DependsOn {
				if keep[d] {
					deps = append(deps, d)
				}
			}
			n.DependsOn = deps
		}
	}
	for id, n := range g.Nodes {
		for _, d := range n.DependsOn {
			g.Nodes[d].Dependents = append(g.Nodes[d].Dependents, id)
		}
	}
	order, err := topo(g)
	if err != nil {
		return nil, err
	}
	g.Order = order
	return g, nil
}

func topo(g *Graph) ([]string, error) {
	indeg := map[string]int{}
	for id, n := range g.Nodes {
		indeg[id] += 0
		for range n.DependsOn {
			indeg[id]++
		}
	}
	var ready []string
	for id, d := range indeg {
		if d == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	var order []string
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		order = append(order, id)
		deps := append([]string(nil), g.Nodes[id].Dependents...)
		sort.Strings(deps)
		for _, d := range deps {
			indeg[d]--
			if indeg[d] == 0 {
				ready = append(ready, d)
				sort.Strings(ready)
			}
		}
	}
	if len(order) != len(g.Nodes) {
		var cyc []string
		for id, d := range indeg {
			if d > 0 {
				cyc = append(cyc, id)
			}
		}
		sort.Strings(cyc)
		return nil, fmt.Errorf("dependency cycle among steps: %s", strings.Join(cyc, ", "))
	}
	return order, nil
}
