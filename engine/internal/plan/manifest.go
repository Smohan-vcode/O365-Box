// Package plan loads provider step manifests and gate definitions and
// builds the step graph for a configuration (SPEC §14.1, §22).
package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Step is one entry of a provider's steps.yaml.
type Step struct {
	ID           string   `yaml:"id"`
	Title        string   `yaml:"title"`
	Track        string   `yaml:"track"`
	Task         string   `yaml:"task"`
	Module       string   `yaml:"module"` // core | records | people | legal | sales | policies | compliance | devices
	DependsOn    []string `yaml:"depends_on"`
	Reads        []string `yaml:"reads"`
	Permissions  []string `yaml:"permissions"`
	Capabilities []string `yaml:"capabilities"`
	Fallback     string   `yaml:"fallback"`      // step used instead when a capability is missing
	FallbackOnly bool     `yaml:"fallback_only"` // runs only as another step's fallback
	CheckOnly    bool     `yaml:"check_only"`    // a gate's check: verify only, never in the build
	Gate         string   `yaml:"gate"`          // gate that must pass before the step
	Optional     bool     `yaml:"optional"`
	Provider     string   `yaml:"-"`
}

// Manifest is a provider's steps.yaml.
type Manifest struct {
	Provider string   `yaml:"provider"`
	Modules  []string `yaml:"modules"` // Microsoft modules the worker imports (one, per S-05)
	Module   string   `yaml:"module_file"`
	Steps    []Step   `yaml:"steps"`
	Dir      string   `yaml:"-"`
}

// Gate is one entry of engine/gates.yaml.
type Gate struct {
	ID        string `yaml:"id"`
	Title     string `yaml:"title"`
	Guide     string `yaml:"guide"`
	Expected  string `yaml:"expected"`
	Check     string `yaml:"check"`      // step whose verify operation decides the gate
	CheckTask string `yaml:"check_task"` // task that builds the check
}

// Catalog is every manifest and gate the engine knows.
type Catalog struct {
	Manifests map[string]*Manifest // by provider
	Steps     map[string]*Step     // by step id
	Gates     map[string]*Gate
}

// Load reads providersDir/*/steps.yaml and gatesFile. A missing gates file
// means no gates.
func Load(providersDir, gatesFile string) (*Catalog, error) {
	c := &Catalog{Manifests: map[string]*Manifest{}, Steps: map[string]*Step{}, Gates: map[string]*Gate{}}
	paths, err := filepath.Glob(filepath.Join(providersDir, "*", "steps.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	for _, p := range paths {
		var m Manifest
		if err := readYAML(p, &m); err != nil {
			return nil, err
		}
		if m.Provider == "" {
			return nil, fmt.Errorf("%s: provider is required", p)
		}
		if len(m.Modules) > 1 {
			return nil, fmt.Errorf("%s: a provider loads at most one Microsoft module (spike S-05)", p)
		}
		if _, dup := c.Manifests[m.Provider]; dup {
			return nil, fmt.Errorf("%s: provider %q is declared twice", p, m.Provider)
		}
		m.Dir = filepath.Dir(p)
		c.Manifests[m.Provider] = &m
		for i := range m.Steps {
			s := &m.Steps[i]
			s.Provider = m.Provider
			if s.ID == "" {
				return nil, fmt.Errorf("%s: step %d has no id", p, i)
			}
			if s.Module == "" {
				s.Module = "core"
			}
			if _, dup := c.Steps[s.ID]; dup {
				return nil, fmt.Errorf("%s: step %q is declared twice", p, s.ID)
			}
			c.Steps[s.ID] = s
		}
	}
	if gatesFile != "" {
		if _, err := os.Stat(gatesFile); err == nil {
			var g struct {
				Gates []Gate `yaml:"gates"`
			}
			if err := readYAML(gatesFile, &g); err != nil {
				return nil, err
			}
			for i := range g.Gates {
				c.Gates[g.Gates[i].ID] = &g.Gates[i]
			}
		}
	}
	return c, nil
}

// ModuleFile is the provider module's path.
func (m *Manifest) ModuleFile() string {
	name := m.Module
	if name == "" {
		name = "Box." + m.Provider + ".psm1"
	}
	return filepath.Join(m.Dir, name)
}

func readYAML(p string, v any) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	return nil
}
