package policy

import (
	"fmt"
	"sort"
	"strings"
)

// Selection is the configuration's policy section.
type Selection struct {
	Frameworks []string    `json:"frameworks"`
	Tier       string      `json:"tier"`  // core | enhanced
	Stage      string      `json:"stage"` // audit | enforce
	Exceptions []Exception `json:"exceptions"`
	// Set from the company's choices and modules (SPEC §24): settings left
	// out with the reason ("choice id=value", "module devices is off") and
	// the variant to apply.
	LeftOut  map[string]string `json:"left_out,omitempty"`
	Variants map[string]string `json:"variants,omitempty"`
	// Choices are the answers in effect; Defaulted the ones taken from
	// the recommended option because the configuration does not answer them.
	Choices   map[string]string `json:"choices,omitempty"`
	Defaulted []string          `json:"defaulted_choices,omitempty"`
	// Values are configuration values the choices supply (dotted path).
	Values map[string]string `json:"values,omitempty"`
	// ChoiceBundles are the permission bundles choices need, with the choice.
	ChoiceBundles map[string]string `json:"choice_bundles,omitempty"`
}

// Exception removes one setting, or every setting that serves only
// excepted requirements.
type Exception struct {
	ID        string `json:"id"` // a setting or requirement id
	Reason    string `json:"reason"`
	Reference string `json:"reference"`
	Ends      string `json:"ends"`
}

// Requirement statuses in the coverage report.
const (
	StatusEnforced          = "enforced"
	StatusEnforcedProcedure = "enforced with procedure"
	StatusProcedure         = "procedure"
	StatusAddon             = "needs add-on"
	StatusNotSelected       = "not selected"
	StatusException         = "exception"
)

// StatusOrder is the order statuses are reported in.
var StatusOrder = []string{StatusEnforced, StatusEnforcedProcedure, StatusProcedure, StatusAddon, StatusNotSelected, StatusException}

// AppliedSetting is a setting to apply, with its effective mode.
type AppliedSetting struct {
	ID           string   `yaml:"id" json:"id"`
	Platform     string   `yaml:"platform" json:"platform"`
	Name         string   `yaml:"name" json:"name"`
	Value        string   `yaml:"value" json:"value"`
	Mode         string   `yaml:"mode" json:"mode"` // enforce | audit | report
	Requirements []string `yaml:"requirements" json:"requirements"`
	Variant      string   `yaml:"variant,omitempty" json:"variant,omitempty"`
}

// AppliedCentre groups the applied settings of one admin centre.
type AppliedCentre struct {
	ID       string           `json:"id"`
	Title    string           `json:"title"`
	Settings []AppliedSetting `json:"settings"`
}

// NotApplied is a setting left out, and why.
type NotApplied struct {
	ID     string `yaml:"id" json:"id"`
	Centre string `yaml:"centre" json:"centre"`
	Name   string `yaml:"name" json:"name"`
	Reason string `yaml:"reason" json:"reason"`
}

// ControlCoverage counts the statuses of one control's requirements.
type ControlCoverage struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Requirements int    `json:"requirements"`
	Enforced     int    `json:"enforced"`
	Procedure    int    `json:"procedure"`
	Addon        int    `json:"needs_addon"`
}

// FrameworkCoverage is one selected framework's coverage.
type FrameworkCoverage struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	MappedAt string            `json:"mapped_at"`
	Note     string            `json:"note,omitempty"`
	Controls []ControlCoverage `json:"controls"`
	Empty    []string          `json:"empty_controls"` // controls no requirement addresses
}

// Result is what a selection resolves to on one plan.
type Result struct {
	Plan       string              `json:"plan"`
	Selection  Selection           `json:"selection"`
	Applied    []AppliedCentre     `json:"applied"` // sorted by centre id; centres with nothing applied are left out
	NotApplied []NotApplied        `json:"not_applied"`
	Status     map[string]string   `json:"status"` // requirement id -> status
	Counts     map[string]int      `json:"counts"` // status -> requirements
	Coverage   []FrameworkCoverage `json:"coverage"`
}

// AppliedCount is the number of settings to apply.
func (r *Result) AppliedCount() int {
	n := 0
	for _, c := range r.Applied {
		n += len(c.Settings)
	}
	return n
}

// Tiers returns the setting tiers a policy tier selects.
func Tiers(tier string) (map[string]bool, error) {
	switch tier {
	case TierCore:
		return map[string]bool{TierCore: true}, nil
	case TierEnhanced:
		return map[string]bool{TierCore: true, TierEnhanced: true}, nil
	}
	return nil, fmt.Errorf("policy tier %q is not core or enhanced", tier)
}

// Resolve keeps a setting when the plan has its capability, its tier is
// selected and no exception removes it. caps is the plan's capability set
// (engine/capabilities.yaml). The catalogue must be valid.
func (c *Catalogue) Resolve(sel Selection, plan string, caps map[string]bool) (*Result, error) {
	tiers, err := Tiers(sel.Tier)
	if err != nil {
		return nil, err
	}
	if sel.Stage != ModeAudit && sel.Stage != ModeEnforce {
		return nil, fmt.Errorf("policy rollout stage %q is not audit or enforce", sel.Stage)
	}
	exSetting := map[string]Exception{}
	exReq := map[string]Exception{}
	for _, e := range sel.Exceptions {
		switch {
		case c.Setting(e.ID) != nil:
			exSetting[e.ID] = e
		case c.Requirement(e.ID) != nil:
			exReq[e.ID] = e
		default:
			return nil, fmt.Errorf("exception %s is neither a setting nor a requirement", e.ID)
		}
	}
	for _, fid := range sel.Frameworks {
		if c.Framework(fid) == nil {
			return nil, fmt.Errorf("framework %s is not in the catalogue", fid)
		}
	}

	res := &Result{Plan: plan, Selection: sel, NotApplied: []NotApplied{}, Applied: []AppliedCentre{}}
	applied := map[string]bool{}
	for _, ce := range c.Centres {
		ac := AppliedCentre{ID: ce.ID, Title: ce.Title}
		for _, s := range ce.Settings {
			skip := func(reason string) {
				res.NotApplied = append(res.NotApplied, NotApplied{ID: s.ID, Centre: ce.ID, Name: s.Name, Reason: reason})
			}
			cp, ok := c.Capabilities[s.Capability]
			if !ok {
				return nil, fmt.Errorf("setting %s needs unknown capability %q", s.ID, s.Capability)
			}
			if !caps[s.Capability] {
				need := cp.Addon
				if need == "" {
					need = cp.Name
				}
				skip("needs " + need)
				continue
			}
			if !tiers[s.Tier] {
				skip(fmt.Sprintf("tier %s not selected", s.Tier))
				continue
			}
			if why, ok := sel.LeftOut[s.ID]; ok {
				skip(why)
				continue
			}
			if e, ok := exSetting[s.ID]; ok {
				skip("exception " + e.Reference)
				continue
			}
			if refs, all := c.allExcepted(s.Requirements, exReq); all {
				skip("exception " + strings.Join(refs, ", "))
				continue
			}
			mode := s.Mode
			if mode == ModeStaged {
				mode = ModeEnforce
				if sel.Stage == ModeAudit {
					mode = ModeAudit
				}
			}
			ac.Settings = append(ac.Settings, AppliedSetting{ID: s.ID, Platform: s.Platform, Name: s.Name,
				Value: s.Value, Mode: mode, Requirements: append([]string(nil), s.Requirements...), Variant: sel.Variants[s.ID]})
			applied[s.ID] = true
		}
		if len(ac.Settings) > 0 {
			res.Applied = append(res.Applied, ac)
		}
	}

	res.Status = map[string]string{}
	res.Counts = map[string]int{}
	for _, r := range c.Requirements {
		st := status(r, applied, exReq)
		res.Status[r.ID] = st
		res.Counts[st]++
	}

	for _, fid := range sel.Frameworks {
		fw := c.Framework(fid)
		fc := FrameworkCoverage{ID: fw.ID, Title: fw.Title, MappedAt: fw.MappedAt, Note: fw.Note,
			Controls: []ControlCoverage{}, Empty: []string{}}
		for _, k := range fw.Controls {
			cc := ControlCoverage{ID: k.ID, Title: k.Title, Requirements: len(k.Requirements)}
			for _, rid := range k.Requirements {
				switch res.Status[rid] {
				case StatusEnforced, StatusEnforcedProcedure:
					cc.Enforced++
				case StatusProcedure:
					cc.Procedure++
				case StatusAddon:
					cc.Addon++
				}
			}
			fc.Controls = append(fc.Controls, cc)
			if len(k.Requirements) == 0 {
				fc.Empty = append(fc.Empty, k.ID)
			}
		}
		res.Coverage = append(res.Coverage, fc)
	}
	return res, nil
}

// allExcepted reports whether every requirement is excepted, with the
// exceptions' references in order and without repeats.
func (c *Catalogue) allExcepted(reqs []string, ex map[string]Exception) ([]string, bool) {
	if len(reqs) == 0 || len(ex) == 0 {
		return nil, false
	}
	var refs []string
	for _, r := range reqs {
		e, ok := ex[r]
		if !ok {
			return nil, false
		}
		if !contains(refs, e.Reference) {
			refs = append(refs, e.Reference)
		}
	}
	return refs, true
}

func status(r Requirement, applied map[string]bool, ex map[string]Exception) string {
	if _, ok := ex[r.ID]; ok {
		return StatusException
	}
	if r.Type == TypeProcedural {
		return StatusProcedure
	}
	for _, s := range r.Settings {
		if applied[s] {
			if r.Type == TypeTechnical {
				return StatusEnforced
			}
			return StatusEnforcedProcedure
		}
	}
	if len(r.Procedures) > 0 {
		return StatusProcedure
	}
	if r.Tier == TierAddon {
		return StatusAddon
	}
	return StatusNotSelected
}

// Summary is one line: applied, not applied and requirement statuses.
func (r *Result) Summary() string {
	var parts []string
	for _, st := range StatusOrder {
		if n := r.Counts[st]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", st, n))
		}
	}
	return fmt.Sprintf("applied %d settings in %d centres; not applied %d; requirements: %s",
		r.AppliedCount(), len(r.Applied), len(r.NotApplied), strings.Join(parts, ", "))
}

// sortedStatuses returns statuses by count, highest first, then by StatusOrder.
func (r *Result) sortedStatuses() []string {
	var out []string
	for _, st := range StatusOrder {
		if r.Counts[st] > 0 {
			out = append(out, st)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return r.Counts[out[a]] > r.Counts[out[b]] })
	return out
}
