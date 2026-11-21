// Package choices loads defaults/choices.yaml (SPEC §24): every decision an
// owner makes in onboarding, with options, a recommendation and effects.
package choices

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Smohan-vcode/O365-Box/engine/internal/report"
)

// Effects of choosing an option.
type Effects struct {
	Modules    []string          `yaml:"modules" json:"modules,omitempty"`
	Variants   map[string]string `yaml:"variants" json:"variants,omitempty"`
	Exceptions []string          `yaml:"exceptions" json:"exceptions,omitempty"`
	Bundles    []string          `yaml:"bundles" json:"bundles,omitempty"`
	Gates      []string          `yaml:"gates" json:"gates,omitempty"`
}

// Option is one answer to a choice.
type Option struct {
	Value           string  `yaml:"value" json:"value"`
	Label           string  `yaml:"label" json:"label"`
	Detail          string  `yaml:"detail" json:"detail,omitempty"`
	Cost            string  `yaml:"cost" json:"cost"`
	When            string  `yaml:"when" json:"when,omitempty"`
	Recommended     bool    `yaml:"recommended" json:"recommended,omitempty"`
	RecommendedWhen string  `yaml:"recommended_when" json:"recommended_when,omitempty"`
	Effects         Effects `yaml:"effects" json:"effects"`
}

// Choice is one decision.
type Choice struct {
	ID       string   `yaml:"id" json:"id"`
	Step     string   `yaml:"step" json:"step"`
	Question string   `yaml:"question" json:"question"`
	Help     string   `yaml:"help" json:"help,omitempty"`
	Detect   string   `yaml:"detect" json:"detect,omitempty"`
	When     string   `yaml:"when" json:"when,omitempty"`
	Options  []Option `yaml:"options" json:"options"`
}

// Catalogue is the loaded file.
type Catalogue struct {
	Choices []Choice `yaml:"choices" json:"choices"`
	File    string   `yaml:"-" json:"-"`
	byID    map[string]*Choice
}

// Load reads the catalogue.
func Load(path string) (*Catalogue, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Catalogue
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.File = path
	c.byID = map[string]*Choice{}
	for i := range c.Choices {
		c.byID[c.Choices[i].ID] = &c.Choices[i]
	}
	return &c, nil
}

// Choice returns a choice by id, or nil.
func (c *Catalogue) Choice(id string) *Choice { return c.byID[id] }

// Option returns an option of a choice, or nil.
func (ch *Choice) Option(v string) *Option {
	for i := range ch.Options {
		if ch.Options[i].Value == v {
			return &ch.Options[i]
		}
	}
	return nil
}

// Known lists the ids the effects may name.
type Known struct {
	Settings map[string]bool
	Variants map[string]map[string]bool // setting -> variant keys present in the mappings
	Modules  map[string]bool
	Bundles  map[string]bool // nil: not checked yet
	Gates    map[string]bool // nil: not checked yet
}

var steps = map[string]bool{"start": true, "company": true, "addresses": true, "security": true, "structure": true, "people": true}

// Validate checks the catalogue's shape and every reference its effects make.
func (c *Catalogue) Validate(k Known) []report.Issue {
	var out []report.Issue
	add := func(path, msg string) {
		out = append(out, report.Issue{Check: "choices", File: "defaults/choices.yaml", Path: path, Message: msg})
	}
	seen := map[string]bool{}
	for _, ch := range c.Choices {
		p := "choices." + ch.ID
		if ch.ID == "" || seen[ch.ID] {
			add(p, "missing or duplicate id")
		}
		seen[ch.ID] = true
		if w := strings.TrimPrefix(ch.When, "module "); ch.When != "" && (w == ch.When || !k.Modules[w]) {
			add(p, fmt.Sprintf("when %q is not \"module <known module>\"", ch.When))
		}
		if !steps[ch.Step] {
			add(p, fmt.Sprintf("unknown step %q", ch.Step))
		}
		if strings.TrimSpace(ch.Question) == "" || len(ch.Options) < 2 {
			add(p, "a choice needs a question and at least two options")
		}
		recs, vals := 0, map[string]bool{}
		for _, o := range ch.Options {
			op := p + "." + o.Value
			if o.Value == "" || vals[o.Value] {
				add(op, "missing or duplicate option value")
			}
			vals[o.Value] = true
			if o.Label == "" || o.Cost == "" {
				add(op, "an option needs a label and a cost (\"none\" when free)")
			}
			if o.Recommended {
				recs++
			}
			for _, m := range o.Effects.Modules {
				if !k.Modules[m] {
					add(op, "unknown module "+m)
				}
			}
			for _, s := range o.Effects.Exceptions {
				if !k.Settings[s] {
					add(op, "unknown setting "+s)
				}
			}
			for s, v := range o.Effects.Variants {
				if !k.Settings[s] {
					add(op, "unknown setting "+s)
				} else if k.Variants != nil && !k.Variants[s][v] {
					add(op, fmt.Sprintf("setting %s has no variant %q in expected-state/setting-ids", s, v))
				}
			}
			if k.Gates != nil {
				for _, g := range o.Effects.Gates {
					if !k.Gates[g] {
						add(op, "unknown gate "+g+" (engine/gates.yaml)")
					}
				}
			}
			if k.Bundles != nil {
				for _, b := range o.Effects.Bundles {
					if !k.Bundles[b] {
						add(op, "unknown permission bundle "+b)
					}
				}
			}
		}
		if recs != 1 {
			add(p, fmt.Sprintf("exactly one option must be recommended (found %d)", recs))
		}
	}
	report.Sort(out)
	return out
}

// Context is what defaults and conditions are evaluated against.
type Context struct {
	Modules  map[string]bool // enabled modules
	Detected map[string]bool // facts discovery found, by detect key; nil when unknown
}

func (ctx Context) holds(cond string) (bool, bool) {
	cond = strings.TrimSpace(cond)
	if cond == "" {
		return true, true
	}
	neg := false
	if strings.HasPrefix(cond, "not ") {
		neg, cond = true, strings.TrimPrefix(cond, "not ")
	}
	var v, known bool
	switch {
	case strings.HasPrefix(cond, "module "):
		v, known = ctx.Modules[strings.TrimPrefix(cond, "module ")], true
	case cond == "detected":
		return false, false // evaluated per choice by the caller
	default:
		return false, false
	}
	if neg {
		v = !v
	}
	return v, known
}

// Applies reports whether a choice is asked for this company.
func (ch *Choice) Applies(ctx Context) bool {
	v, known := ctx.holds(ch.When)
	return v || !known
}

// Default is the option preselected for a company: the first option whose
// recommended_when holds, else the recommended one. A fact discovery has
// not reported counts as not detected.
func (ch *Choice) Default(ctx Context) string {
	detected := ch.Detect != "" && ctx.Detected[ch.Detect]
	eligible := func(o Option) bool { return o.When != "detected" || detected }
	for _, o := range ch.Options {
		if o.RecommendedWhen == "" || !eligible(o) {
			continue
		}
		cond := o.RecommendedWhen
		if strings.TrimPrefix(cond, "not ") == "detected" {
			if detected != strings.HasPrefix(cond, "not ") {
				return o.Value
			}
			continue
		}
		if v, known := ctx.holds(cond); known && v {
			return o.Value
		}
	}
	for _, o := range ch.Options {
		if o.Recommended && eligible(o) {
			return o.Value
		}
	}
	for _, o := range ch.Options {
		if eligible(o) {
			return o.Value
		}
	}
	return ch.Options[0].Value
}

// Resolved is the effect of a company's answers.
type Resolved struct {
	Answers   map[string]string // every applicable choice, answered or defaulted
	Defaulted map[string]bool   // answers taken from the default
	LeftOut   map[string]string // setting -> "choice id=value"
	Variants  map[string]string // setting -> variant key
	Modules   map[string]bool
	Bundles   map[string]bool
	Gates     map[string]bool
}

// Resolve applies the answers (unanswered applicable choices take their
// default) and gathers their effects.
func (c *Catalogue) Resolve(answers map[string]string, ctx Context) (*Resolved, error) {
	r := &Resolved{Answers: map[string]string{}, Defaulted: map[string]bool{}, LeftOut: map[string]string{},
		Variants: map[string]string{}, Modules: map[string]bool{}, Bundles: map[string]bool{}, Gates: map[string]bool{}}
	ids := make([]string, 0, len(answers))
	for id := range answers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		ch := c.Choice(id)
		if ch == nil {
			return nil, fmt.Errorf("unknown choice %q", id)
		}
		if ch.Option(answers[id]) == nil {
			return nil, fmt.Errorf("choice %s has no option %q", id, answers[id])
		}
	}
	for i := range c.Choices {
		ch := &c.Choices[i]
		if !ch.Applies(ctx) {
			continue
		}
		v, ok := answers[ch.ID]
		if !ok {
			v = ch.Default(ctx)
			r.Defaulted[ch.ID] = true
		}
		r.Answers[ch.ID] = v
		o := ch.Option(v)
		for _, m := range o.Effects.Modules {
			r.Modules[m] = true
		}
		for _, s := range o.Effects.Exceptions {
			r.LeftOut[s] = ch.ID + "=" + v
		}
		for s, k := range o.Effects.Variants {
			r.Variants[s] = k
		}
		for _, b := range o.Effects.Bundles {
			r.Bundles[b] = true
		}
		for _, g := range o.Effects.Gates {
			r.Gates[g] = true
		}
	}
	return r, nil
}
