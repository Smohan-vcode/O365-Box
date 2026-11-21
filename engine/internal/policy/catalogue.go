// Package policy loads the policy catalogue (policy/), checks its rules
// and resolves a company's selection into the settings to apply and the
// coverage each selected framework gets (policy/README.md).
package policy

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Tiers, requirement types and setting modes the catalogue may use.
const (
	TierCore     = "core"
	TierEnhanced = "enhanced"
	TierAddon    = "addon"

	TypeTechnical  = "technical"
	TypeProcedural = "procedural"
	TypeMixed      = "mixed"

	ModeEnforce = "enforce"
	ModeStaged  = "staged"
	ModeReport  = "report"
	ModeAudit   = "audit"
)

// Requirement is one entry of policy/requirements.yaml.
type Requirement struct {
	ID         string              `yaml:"id" json:"id"`
	Domain     string              `yaml:"domain" json:"domain"`
	Statement  string              `yaml:"statement" json:"statement"`
	Tier       string              `yaml:"tier" json:"tier"`
	Type       string              `yaml:"type" json:"type"`
	Frameworks map[string][]string `yaml:"frameworks" json:"frameworks"`
	Settings   []string            `yaml:"settings" json:"settings"`
	Evidence   string              `yaml:"evidence" json:"evidence"`
	Procedures []string            `yaml:"procedures" json:"procedures,omitempty"`
	Line       int                 `yaml:"-" json:"-"`
}

// Setting is one entry of a policy/settings/<centre>.yaml file.
type Setting struct {
	ID           string   `yaml:"id" json:"id"`
	Platform     string   `yaml:"platform" json:"platform"`
	Name         string   `yaml:"name" json:"name"`
	Value        string   `yaml:"value" json:"value"`
	Tier         string   `yaml:"tier" json:"tier"`
	Mode         string   `yaml:"mode" json:"mode"`
	Capability   string   `yaml:"capability" json:"capability"`
	Requirements []string `yaml:"requirements" json:"requirements"`
	Centre       string   `yaml:"-" json:"centre"`
	Line         int      `yaml:"-" json:"-"`
}

// Centre is one admin centre: one file under policy/settings/.
type Centre struct {
	ID       string    // file name without .yaml
	Title    string    // admin_centre
	File     string    // path relative to the repository root
	Settings []Setting // in file order
}

// Control is one framework control and the requirements that answer it.
type Control struct {
	ID           string   `yaml:"id"`
	Title        string   `yaml:"title"`
	Requirements []string `yaml:"requirements"`
	Line         int      `yaml:"-"`
}

// Framework is one policy/frameworks/<id>.yaml file.
type Framework struct {
	ID       string    `yaml:"id"`
	Title    string    `yaml:"title"`
	MappedAt string    `yaml:"mapped_at"`
	Note     string    `yaml:"note"`
	Controls []Control `yaml:"controls"`
	File     string    `yaml:"-"`
}

// Capability is one entry of policy/capabilities.yaml.
type Capability struct {
	Name       string   `yaml:"name"`
	IncludedIn []string `yaml:"included_in"`
	Addon      string   `yaml:"addon"`
}

// Catalogue is the whole of policy/.
type Catalogue struct {
	Dir          string // absolute path of policy/
	Rel          string // policy/ relative to the repository root, for messages
	Requirements []Requirement
	Centres      []Centre    // sorted by ID
	Frameworks   []Framework // sorted by ID
	Capabilities map[string]Capability
	CapLines     map[string]int
	// CapabilityRule is capabilities.yaml's rule on users whose plan lacks a capability.
	CapabilityRule string

	reqs     map[string]*Requirement
	settings map[string]*Setting
	fws      map[string]*Framework
}

// File names inside the catalogue directory.
const (
	RequirementsFile = "requirements.yaml"
	CapabilitiesFile = "capabilities.yaml"
	SettingsDir      = "settings"
	FrameworksDir    = "frameworks"
)

// Load reads the catalogue in dir. rel names dir in messages (for example
// "policy").
func Load(dir, rel string) (*Catalogue, error) {
	c := &Catalogue{Dir: dir, Rel: rel}
	var reqs struct {
		Requirements []Requirement `yaml:"requirements"`
	}
	if err := readYAML(filepath.Join(dir, RequirementsFile), &reqs); err != nil {
		return nil, err
	}
	c.Requirements = reqs.Requirements

	var caps struct {
		Capabilities yaml.Node `yaml:"capabilities"`
		Rule         string    `yaml:"rule"`
	}
	if err := readYAML(filepath.Join(dir, CapabilitiesFile), &caps); err != nil {
		return nil, err
	}
	c.CapabilityRule = caps.Rule
	c.Capabilities = map[string]Capability{}
	c.CapLines = map[string]int{}
	if n := caps.Capabilities; n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			var cp Capability
			if err := n.Content[i+1].Decode(&cp); err != nil {
				return nil, fmt.Errorf("%s: %w", filepath.Join(rel, CapabilitiesFile), err)
			}
			c.Capabilities[n.Content[i].Value] = cp
			c.CapLines[n.Content[i].Value] = n.Content[i].Line
		}
	}

	files, err := yamlFiles(filepath.Join(dir, SettingsDir))
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		var doc struct {
			AdminCentre string    `yaml:"admin_centre"`
			Settings    []Setting `yaml:"settings"`
		}
		if err := readYAML(filepath.Join(dir, SettingsDir, f), &doc); err != nil {
			return nil, err
		}
		id := strings.TrimSuffix(f, ".yaml")
		for i := range doc.Settings {
			doc.Settings[i].Centre = id
		}
		c.Centres = append(c.Centres, Centre{ID: id, Title: doc.AdminCentre,
			File: filepath.ToSlash(filepath.Join(rel, SettingsDir, f)), Settings: doc.Settings})
	}

	files, err = yamlFiles(filepath.Join(dir, FrameworksDir))
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		var fw Framework
		if err := readYAML(filepath.Join(dir, FrameworksDir, f), &fw); err != nil {
			return nil, err
		}
		fw.File = filepath.ToSlash(filepath.Join(rel, FrameworksDir, f))
		c.Frameworks = append(c.Frameworks, fw)
	}
	sort.Slice(c.Frameworks, func(a, b int) bool { return c.Frameworks[a].ID < c.Frameworks[b].ID })
	c.index()
	return c, nil
}

func (c *Catalogue) index() {
	c.reqs = map[string]*Requirement{}
	for i := range c.Requirements {
		if _, dup := c.reqs[c.Requirements[i].ID]; !dup {
			c.reqs[c.Requirements[i].ID] = &c.Requirements[i]
		}
	}
	c.settings = map[string]*Setting{}
	for ci := range c.Centres {
		for si := range c.Centres[ci].Settings {
			s := &c.Centres[ci].Settings[si]
			if _, dup := c.settings[s.ID]; !dup {
				c.settings[s.ID] = s
			}
		}
	}
	c.fws = map[string]*Framework{}
	for i := range c.Frameworks {
		if _, dup := c.fws[c.Frameworks[i].ID]; !dup {
			c.fws[c.Frameworks[i].ID] = &c.Frameworks[i]
		}
	}
}

// Requirement returns the requirement with id, or nil.
func (c *Catalogue) Requirement(id string) *Requirement { return c.reqs[id] }

// Setting returns the setting with id, or nil.
func (c *Catalogue) Setting(id string) *Setting { return c.settings[id] }

// Framework returns the framework with id, or nil.
func (c *Catalogue) Framework(id string) *Framework { return c.fws[id] }

// IDs lists framework, setting and requirement ids, each sorted.
func (c *Catalogue) IDs() (frameworks, settings, requirements []string) {
	frameworks, settings, requirements = []string{}, []string{}, []string{}
	for id := range c.fws {
		frameworks = append(frameworks, id)
	}
	for id := range c.settings {
		settings = append(settings, id)
	}
	for id := range c.reqs {
		requirements = append(requirements, id)
	}
	sort.Strings(frameworks)
	sort.Strings(settings)
	sort.Strings(requirements)
	return
}

// SettingCount is the number of settings in the catalogue.
func (c *Catalogue) SettingCount() int {
	n := 0
	for _, ce := range c.Centres {
		n += len(ce.Settings)
	}
	return n
}

// UnmarshalYAML records the line of each requirement.
func (r *Requirement) UnmarshalYAML(n *yaml.Node) error {
	type plain Requirement
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*r = Requirement(p)
	r.Line = n.Line
	return nil
}

// UnmarshalYAML records the line of each setting.
func (s *Setting) UnmarshalYAML(n *yaml.Node) error {
	type plain Setting
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*s = Setting(p)
	s.Line = n.Line
	return nil
}

// UnmarshalYAML records the line of each control.
func (k *Control) UnmarshalYAML(n *yaml.Node) error {
	type plain Control
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*k = Control(p)
	k.Line = n.Line
	return nil
}

func readYAML(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func yamlFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}
