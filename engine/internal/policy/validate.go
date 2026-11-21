package policy

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Smohan-vcode/O365-Box/engine/internal/report"
)

// Validate checks the catalogue's rules (policy/README.md). plans maps
// each plan to its capability names (engine/capabilities.yaml); nil skips
// the cross-check between the two capability files.
func (c *Catalogue) Validate(plans map[string][]string) []report.Issue {
	v := &checker{c: c}
	v.requirements()
	v.settings()
	v.frameworks()
	v.capabilities(plans)
	report.Sort(v.issues)
	return v.issues
}

type checker struct {
	c      *Catalogue
	issues []report.Issue
}

func (v *checker) add(file string, line int, id, format string, a ...any) {
	v.issues = append(v.issues, report.Issue{Check: "policy", File: file, Line: line, Path: id,
		Message: fmt.Sprintf(format, a...)})
}

func (v *checker) reqFile() string { return filepath.ToSlash(filepath.Join(v.c.Rel, RequirementsFile)) }
func (v *checker) capFile() string { return filepath.ToSlash(filepath.Join(v.c.Rel, CapabilitiesFile)) }

func (v *checker) requirements() {
	seen := map[string]bool{}
	for _, r := range v.c.Requirements {
		f := v.reqFile()
		if r.ID == "" {
			v.add(f, r.Line, "", "requirement has no id")
			continue
		}
		if seen[r.ID] {
			v.add(f, r.Line, r.ID, "duplicate requirement id %q", r.ID)
		}
		seen[r.ID] = true
		if r.Statement == "" {
			v.add(f, r.Line, r.ID, "requirement %s has no statement", r.ID)
		}
		if !oneOf(r.Tier, TierCore, TierEnhanced, TierAddon) {
			v.add(f, r.Line, r.ID, "requirement %s: tier %q is not core, enhanced or addon", r.ID, r.Tier)
		}
		if !oneOf(r.Type, TypeTechnical, TypeProcedural, TypeMixed) {
			v.add(f, r.Line, r.ID, "requirement %s: type %q is not technical, procedural or mixed", r.ID, r.Type)
		}
		if r.Type == TypeTechnical && len(r.Settings) == 0 {
			v.add(f, r.Line, r.ID, "technical requirement %s has no setting", r.ID)
		}
		for _, sid := range r.Settings {
			s := v.c.Setting(sid)
			switch {
			case s == nil:
				v.add(f, r.Line, r.ID, "requirement %s lists unknown setting %s", r.ID, sid)
			case !contains(s.Requirements, r.ID):
				v.add(f, r.Line, r.ID, "requirement %s lists setting %s, which does not name it", r.ID, sid)
			}
		}
		for _, fid := range sortedKeys(r.Frameworks) {
			fw := v.c.Framework(fid)
			if fw == nil {
				v.add(f, r.Line, r.ID, "requirement %s refers to unknown framework %s", r.ID, fid)
				continue
			}
			for _, ref := range r.Frameworks[fid] {
				k := fw.control(ref)
				switch {
				case k == nil:
					v.add(f, r.Line, r.ID, "requirement %s: %s %s is not a control in %s", r.ID, fid, ref, fw.File)
				case !contains(k.Requirements, r.ID):
					v.add(f, r.Line, r.ID, "requirement %s refers to %s %s, whose control does not list it", r.ID, fid, ref)
				}
			}
		}
	}
}

func (v *checker) settings() {
	seen := map[string]string{}
	for _, ce := range v.c.Centres {
		if ce.Title == "" {
			v.add(ce.File, 0, "", "admin_centre is empty")
		}
		for _, s := range ce.Settings {
			if s.ID == "" {
				v.add(ce.File, s.Line, "", "setting has no id")
				continue
			}
			if prev, dup := seen[s.ID]; dup {
				v.add(ce.File, s.Line, s.ID, "duplicate setting id %q (also in %s)", s.ID, prev)
			}
			seen[s.ID] = ce.File
			if v.c.Requirement(s.ID) != nil {
				v.add(ce.File, s.Line, s.ID, "setting id %s is also a requirement id", s.ID)
			}
			if s.Name == "" || s.Value == "" {
				v.add(ce.File, s.Line, s.ID, "setting %s needs a name and a value", s.ID)
			}
			if !oneOf(s.Tier, TierCore, TierEnhanced, TierAddon) {
				v.add(ce.File, s.Line, s.ID, "setting %s: tier %q is not core, enhanced or addon", s.ID, s.Tier)
			}
			if !oneOf(s.Mode, ModeEnforce, ModeStaged, ModeReport) {
				v.add(ce.File, s.Line, s.ID, "setting %s: mode %q is not enforce, staged or report", s.ID, s.Mode)
			}
			cp, ok := v.c.Capabilities[s.Capability]
			if !ok {
				v.add(ce.File, s.Line, s.ID, "setting %s needs unknown capability %q", s.ID, s.Capability)
			} else if s.Tier == TierAddon && len(cp.IncludedIn) > 0 {
				v.add(ce.File, s.Line, s.ID, "setting %s is tier addon but capability %s is included in %s",
					s.ID, s.Capability, strings.Join(cp.IncludedIn, ", "))
			}
			if len(s.Requirements) == 0 {
				v.add(ce.File, s.Line, s.ID, "setting %s serves no requirement", s.ID)
			}
			for _, rid := range s.Requirements {
				r := v.c.Requirement(rid)
				switch {
				case r == nil:
					v.add(ce.File, s.Line, s.ID, "setting %s refers to unknown requirement %s", s.ID, rid)
				case !contains(r.Settings, s.ID):
					v.add(ce.File, s.Line, s.ID, "setting %s names requirement %s, which does not list it", s.ID, rid)
				}
			}
		}
	}
}

func (v *checker) frameworks() {
	seen := map[string]bool{}
	for _, fw := range v.c.Frameworks {
		want := strings.TrimSuffix(filepath.Base(fw.File), ".yaml")
		if fw.ID != want {
			v.add(fw.File, 0, fw.ID, "framework id %q does not match the file name %q", fw.ID, want)
		}
		if seen[fw.ID] {
			v.add(fw.File, 0, fw.ID, "duplicate framework id %q", fw.ID)
		}
		seen[fw.ID] = true
		if fw.Title == "" {
			v.add(fw.File, 0, fw.ID, "framework %s has no title", fw.ID)
		}
		controls := map[string]bool{}
		for ki := range fw.Controls {
			k := &fw.Controls[ki]
			if controls[k.ID] {
				v.add(fw.File, k.Line, k.ID, "duplicate control %q", k.ID)
			}
			controls[k.ID] = true
			for _, rid := range k.Requirements {
				r := v.c.Requirement(rid)
				if r == nil {
					v.add(fw.File, k.Line, k.ID, "control %s refers to unknown requirement %s", k.ID, rid)
					continue
				}
				found := false
				for _, ref := range r.Frameworks[fw.ID] {
					found = found || fw.control(ref) == k
				}
				if !found {
					v.add(fw.File, k.Line, k.ID, "control %s lists requirement %s, which has no %s reference to it", k.ID, rid, fw.ID)
				}
			}
		}
	}
}

func (v *checker) capabilities(plans map[string][]string) {
	f := v.capFile()
	for _, id := range sortedKeys(v.c.Capabilities) {
		cp := v.c.Capabilities[id]
		line := v.c.CapLines[id]
		if cp.Name == "" {
			v.add(f, line, id, "capability %s has no name", id)
		}
		if len(cp.IncludedIn) == 0 && cp.Addon == "" {
			v.add(f, line, id, "capability %s is in no plan and names no add-on", id)
		}
		if plans == nil {
			continue
		}
		for _, p := range cp.IncludedIn {
			list, ok := plans[p]
			switch {
			case !ok:
				v.add(f, line, id, "capability %s: plan %s is not in engine/capabilities.yaml", id, p)
			case !contains(list, id):
				v.add(f, line, id, "capability %s is included in %s here but not in engine/capabilities.yaml", id, p)
			}
		}
		for _, p := range sortedKeys(plans) {
			if contains(plans[p], id) && !contains(cp.IncludedIn, p) {
				v.add(f, line, id, "engine/capabilities.yaml gives plan %s capability %s, but included_in does not list it", p, id)
			}
		}
	}
}

// control returns the control a requirement's reference points at: the
// control with that id, or the control whose id is the reference's prefix
// before a hyphen (NIST CSF categories hold subcategory references, for
// example PR.AA holds PR.AA-03).
func (fw *Framework) control(ref string) *Control {
	for i := range fw.Controls {
		if fw.Controls[i].ID == ref {
			return &fw.Controls[i]
		}
	}
	for i := range fw.Controls {
		if strings.HasPrefix(ref, fw.Controls[i].ID+"-") {
			return &fw.Controls[i]
		}
	}
	return nil
}

func oneOf(s string, opts ...string) bool { return contains(opts, s) }

func contains(s []string, x string) bool {
	for _, e := range s {
		if e == x {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
