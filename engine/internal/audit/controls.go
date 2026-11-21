package audit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
)

// Control and requirement statuses (SPEC §26). "not read" is used when no
// evidence for a technical requirement could be read: the pack says so
// instead of guessing.
const (
	Met            = "met"
	PartlyMet      = "partly met"
	NotMet         = "not met"
	DocumentNeeded = "document needed"
)

// statusRisk orders gaps: not met first.
var statusRisk = map[string]int{NotMet: 4, PartlyMet: 3, NotRead: 2, DocumentNeeded: 1, Met: 0}

var tierWeight = map[string]int{policy.TierCore: 3, policy.TierEnhanced: 2, policy.TierAddon: 1}

var severityWeight = map[string]int{High: 3, Medium: 2, Low: 1}

// RequirementStatus is one requirement's assessment.
type RequirementStatus struct {
	ID          string         `json:"id"`
	Domain      string         `json:"domain"`
	Statement   string         `json:"statement"`
	Type        string         `json:"type"`
	Tier        string         `json:"tier"`
	Status      string         `json:"status"`
	Evidence    string         `json:"evidence"` // the catalogue's evidence source (collector.* or task.*)
	Settings    []*SettingRead `json:"settings,omitempty"`
	Inventories []string       `json:"inventories,omitempty"`
	Checks      []Check        `json:"checks,omitempty"`
	Procedures  []string       `json:"procedures,omitempty"`
	NeedsDoc    bool           `json:"document_needed"`
	// Document is the written procedure's state when the policy library
	// was read (DocInPlace, DocDraft, ...); Documents names the documents.
	Document  string      `json:"document,omitempty"`
	Documents string      `json:"documents,omitempty"`
	Records   []RecordRef `json:"records,omitempty"`
	Gap       string      `json:"gap,omitempty"`
	Fix       string      `json:"fix,omitempty"`
	Risk      int         `json:"risk"`
}

// ControlStatus is one framework control's row in the pack.
type ControlStatus struct {
	Framework    string               `json:"framework"`
	ID           string               `json:"id"`
	Title        string               `json:"title,omitempty"`
	Status       string               `json:"status"`
	Requirements []*RequirementStatus `json:"requirements"`
	Documents    []string             `json:"documents_needed,omitempty"` // requirement ids that need a written procedure
	Evidence     []string             `json:"evidence"`                   // file names under evidence/<control>/
	Gap          string               `json:"gap,omitempty"`
}

// Assessment is the framework's result.
type Assessment struct {
	Framework    *policy.Framework
	Controls     []*ControlStatus
	Requirements map[string]*RequirementStatus
	Counts       map[string]int
}

// collectorOf returns the collector name of a requirement's evidence
// ("collector.ca_policies" -> "ca_policies"), or "".
func collectorOf(ev string) string {
	if c, ok := strings.CutPrefix(ev, "collector."); ok {
		return c
	}
	return ""
}

// Assess gives every control of the framework a status from its
// requirements' setting reads and inventories.
func Assess(cat *policy.Catalogue, fw *policy.Framework, settings map[string]*SettingRead, inventories []*Inventory) *Assessment {
	return AssessWith(cat, fw, settings, inventories, nil)
}

// AssessWith is Assess with the company's policy documents and control
// task records read from the tenant (ReadPolicies). nil leaves every
// requirement that needs a written procedure as "document needed".
func AssessWith(cat *policy.Catalogue, fw *policy.Framework, settings map[string]*SettingRead, inventories []*Inventory, recs *Records) *Assessment {
	byCollector := map[string][]*Inventory{}
	for _, inv := range inventories {
		for _, c := range inv.Collectors {
			byCollector[c] = append(byCollector[c], inv)
		}
	}
	a := &Assessment{Framework: fw, Requirements: map[string]*RequirementStatus{}, Counts: map[string]int{}}
	reqStatus := func(id string) *RequirementStatus {
		if rs, ok := a.Requirements[id]; ok {
			return rs
		}
		r := cat.Requirement(id)
		if r == nil {
			rs := &RequirementStatus{ID: id, Status: NotRead, Gap: "requirement " + id + " is not in the catalogue"}
			a.Requirements[id] = rs
			return rs
		}
		rs := assessRequirement(r, settings, byCollector[collectorOf(r.Evidence)])
		if recs != nil && rs.NeedsDoc {
			recs.applyDocuments(rs, r)
		}
		a.Requirements[id] = rs
		return rs
	}
	for _, k := range fw.Controls {
		cs := &ControlStatus{Framework: fw.ID, ID: k.ID, Title: k.Title}
		for _, id := range k.Requirements {
			cs.Requirements = append(cs.Requirements, reqStatus(id))
		}
		controlStatus(cs)
		a.Counts[cs.Status]++
		a.Controls = append(a.Controls, cs)
	}
	return a
}

func assessRequirement(r *policy.Requirement, settings map[string]*SettingRead, invs []*Inventory) *RequirementStatus {
	rs := &RequirementStatus{ID: r.ID, Domain: r.Domain, Statement: r.Statement, Type: r.Type, Tier: r.Tier, Evidence: r.Evidence,
		Procedures: r.Procedures, NeedsDoc: r.Type != policy.TypeTechnical}
	for _, id := range r.Settings {
		if s := settings[id]; s != nil {
			rs.Settings = append(rs.Settings, s)
		}
	}
	invRead := false
	for _, inv := range invs {
		rs.Inventories = append(rs.Inventories, inv.ID)
		if inv.Status != NotRead {
			invRead = true
			for _, c := range inv.Checks {
				if len(c.Collectors) == 0 || contains(c.Collectors, collectorOf(r.Evidence)) {
					rs.Checks = append(rs.Checks, c)
				}
			}
		}
	}
	if r.Type == policy.TypeProcedural {
		rs.Status = DocumentNeeded
		rs.Gap = "needs a written, approved procedure and proof it runs (" + docWhat(r) + ")"
		rs.Fix = fixProcedure(r)
		rs.Risk = statusRisk[DocumentNeeded] * tierWeight[r.Tier]
		return rs
	}
	var pass, fail, notRead []string
	for _, s := range rs.Settings {
		switch s.Status {
		case Pass:
			pass = append(pass, s.ID)
		case Fail:
			fail = append(fail, s.ID)
		default:
			notRead = append(notRead, s.ID)
		}
	}
	var good, bad []Check
	high := 0
	for _, c := range rs.Checks {
		if c.OK {
			good = append(good, c)
		} else {
			bad = append(bad, c)
			high += severityWeight[c.Severity]
		}
	}
	np, nf := len(pass)+len(good), len(fail)+len(bad)
	switch {
	case np+nf == 0:
		rs.Status = NotRead
	case nf == 0 && len(notRead) == 0:
		rs.Status = Met
	case nf == 0:
		rs.Status = PartlyMet // what was read holds; the rest could not be read
	case np == 0:
		rs.Status = NotMet
	default:
		rs.Status = PartlyMet
	}
	var gap []string
	for _, s := range rs.Settings {
		if s.Status == Fail {
			gap = append(gap, fmt.Sprintf("%s %s is not %q", s.ID, s.Name, s.Expected))
		}
	}
	for _, c := range bad {
		gap = append(gap, c.Text)
	}
	if len(notRead) > 0 {
		gap = append(gap, fmt.Sprintf("%d setting(s) not read (%s)", len(notRead), strings.Join(notRead, ", ")))
	}
	if np+nf == 0 && !invRead && len(invs) > 0 {
		gap = append(gap, "evidence not read: "+invs[0].Reason)
	}
	if rs.Status != Met {
		rs.Gap = strings.Join(gap, "; ")
		rs.Fix = fixTechnical(r, rs.Settings)
	}
	rs.Risk = statusRisk[rs.Status]*tierWeight[r.Tier] + high + len(fail)
	if r.Type == policy.TypeMixed {
		doc := "and a written, approved procedure (" + docWhat(r) + ")"
		if rs.Status == Met {
			rs.Gap, rs.Fix = "the settings hold; it also needs "+strings.TrimPrefix(doc, "and "), fixProcedure(r)
			rs.Risk = statusRisk[DocumentNeeded] * tierWeight[r.Tier]
		} else {
			rs.Gap += "; " + doc
		}
	}
	return rs
}

func docWhat(r *policy.Requirement) string {
	if len(r.Procedures) > 0 {
		return strings.Join(r.Procedures, ", ")
	}
	if t, ok := strings.CutPrefix(r.Evidence, "task."); ok {
		return "recurring task " + t
	}
	return "a written policy"
}

func fixProcedure(r *policy.Requirement) string {
	return "Fix with Company in a Box: tick the framework in the onboarding's Security step; it creates " + docWhat(r) +
		" as a recurring task with its record, and the written standard for " + r.Domain + " is rendered from requirement " + r.ID + "."
}

func fixTechnical(r *policy.Requirement, settings []*SettingRead) string {
	byStep := map[string][]string{}
	var manual []string
	for _, s := range settings {
		if s.Status == Pass {
			continue
		}
		if s.Step != "" && s.Provider != "" && !strings.HasPrefix(s.Detail, "a person confirms") && !strings.HasPrefix(s.Detail, "needs a paid add-on") {
			byStep[s.Step] = append(byStep[s.Step], s.ID)
		} else {
			manual = append(manual, s.ID)
		}
	}
	steps := make([]string, 0, len(byStep))
	for st := range byStep {
		steps = append(steps, st+" ("+strings.Join(byStep[st], ", ")+")")
	}
	sort.Strings(steps)
	var parts []string
	if len(steps) > 0 {
		parts = append(parts, "the proposal applies "+strings.Join(steps, "; ")+"; run the onboarding (box), then box plan shows each change before it is made")
	}
	if len(manual) > 0 {
		parts = append(parts, "guided steps (gates) for "+strings.Join(manual, ", "))
	}
	if len(parts) == 0 {
		return "Fix with Company in a Box: the onboarding's Security step selects requirement " + r.ID + "."
	}
	return "Fix with Company in a Box: " + strings.Join(parts, "; ") + "."
}

func controlStatus(cs *ControlStatus) {
	var tech []string
	var gaps []string
	for _, r := range cs.Requirements {
		if r.NeedsDoc {
			cs.Documents = append(cs.Documents, r.ID)
		}
		if r.Type == policy.TypeProcedural && r.Status == DocumentNeeded {
			continue
		}
		tech = append(tech, r.Status)
		if r.Status != Met && r.Gap != "" {
			gaps = append(gaps, r.ID+": "+r.Gap)
		}
	}
	count := map[string]int{}
	for _, s := range tech {
		count[s]++
	}
	switch {
	case len(tech) == 0:
		cs.Status = DocumentNeeded
		cs.Gap = "procedures only: " + strings.Join(cs.Documents, ", ") + " need a written, approved procedure"
		for _, r := range cs.Requirements {
			if r.Document != "" && r.Document != DocNone {
				cs.Gap += "; " + r.ID + ": " + r.Gap
			}
		}
		return
	case count[NotRead] == len(tech):
		cs.Status = NotRead
	case count[Met] == len(tech):
		cs.Status = Met
	case count[NotMet]+count[NotRead] == len(tech) && count[NotMet] > 0:
		cs.Status = NotMet
	default:
		cs.Status = PartlyMet
	}
	if len(cs.Documents) > 0 {
		// Met needs the written procedures too.
		if cs.Status == Met {
			cs.Status = PartlyMet
		}
		gaps = append(gaps, "documents needed: "+strings.Join(cs.Documents, ", "))
	}
	cs.Gap = strings.Join(gaps, " | ")
}

// Gaps are the requirements not met in the framework, by risk (highest
// first), each with the controls it affects.
type Gap struct {
	Requirement *RequirementStatus `json:"requirement"`
	Controls    []string           `json:"controls"`
}

// Gaps lists the requirements whose status is not met, ordered by risk.
func (a *Assessment) Gaps() []Gap {
	ctl := map[string][]string{}
	for _, c := range a.Controls {
		for _, r := range c.Requirements {
			ctl[r.ID] = append(ctl[r.ID], c.ID)
		}
	}
	var out []Gap
	for id, r := range a.Requirements {
		if r.Status == Met && !(r.NeedsDoc && r.Type == policy.TypeMixed) {
			continue
		}
		out = append(out, Gap{Requirement: r, Controls: ctl[id]})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Requirement, out[j].Requirement
		if a.Risk != b.Risk {
			return a.Risk > b.Risk
		}
		return a.ID < b.ID
	})
	return out
}
