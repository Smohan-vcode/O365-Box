package audit

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
)

// Setting read statuses.
const (
	Pass = "pass"
	Fail = "fail"
	// NotRead (shared with inventories) is used for settings too.
)

// SettingRead is one catalogue setting's live state.
type SettingRead struct {
	ID       string `json:"id"`
	Centre   string `json:"centre"`
	Name     string `json:"name"`
	Expected string `json:"expected"`
	Tier     string `json:"tier"`
	Step     string `json:"step,omitempty"`
	Provider string `json:"provider,omitempty"`
	Status   string `json:"status"` // pass | fail | not read
	Detail   string `json:"detail,omitempty"`
	Query    string `json:"query,omitempty"` // the mapping's read
	Check    string `json:"check,omitempty"` // the mapping's check
}

// StepOutcome is what one policy step's verify returned in audit mode.
type StepOutcome struct {
	Step     string          `json:"step"`
	Provider string          `json:"provider"`
	Ran      bool            `json:"ran"`
	Reason   string          `json:"reason,omitempty"` // why it did not run, or its error
	Test     json.RawMessage `json:"test,omitempty"`
	Verify   json.RawMessage `json:"verify,omitempty"`
	// Warnings the step logged while verifying (settings it left out).
	Warnings []string `json:"warnings,omitempty"`
}

// providerNeeds says what reading a provider's settings takes when the
// audit's Graph sign-in cannot.
var providerNeeds = map[string]string{
	"exchange":      "Exchange Online PowerShell with a delegated sign-in (Global Reader or View-Only Organization Management)",
	"sharepoint":    "SharePoint Online management (PnP) with a delegated sign-in to the tenant's admin site; tenant sharing is in the sharing_settings inventory",
	"teams":         "Microsoft Teams PowerShell with a delegated sign-in (Teams Administrator or Global Reader)",
	"powerplatform": "the Power Platform admin API with a Power Platform administrator sign-in",
	"dns":           "public DNS; SPF and DMARC are in the mail_auth inventory",
	"purview":       "Security & Compliance PowerShell with a delegated sign-in (a Purview reader role)",
}

// GraphProviders are the providers whose policy steps read over Microsoft
// Graph only, so their verify runs with the audit's delegated token.
var GraphProviders = map[string]bool{"identity": true, "intune": true}

// NeedsFor returns what reading a step of provider takes.
func NeedsFor(provider, step string) string {
	if strings.HasPrefix(step, "policy.purview") && provider == "exchange" {
		return providerNeeds["purview"]
	}
	if n, ok := providerNeeds[provider]; ok {
		return n
	}
	return "the " + provider + " provider"
}

// AuditSelection lists every resolved catalogue setting grouped by the
// policy step that verifies it, in its enforced form (staged settings are
// checked as enforced, report settings as reported): an audit reads the
// whole catalogue, whatever a company selected.
func AuditSelection(cat *policy.Catalogue, maps map[string]policy.Mapping) map[string][]policy.StepSetting {
	res := &policy.Result{}
	for _, c := range cat.Centres {
		ac := policy.AppliedCentre{ID: c.ID, Title: c.Title}
		for _, s := range c.Settings {
			if m, ok := maps[s.ID]; !ok || m.Status != "resolved" {
				continue
			}
			mode := s.Mode
			if mode == policy.ModeStaged {
				mode = policy.ModeEnforce
			}
			ac.Settings = append(ac.Settings, policy.AppliedSetting{ID: s.ID, Platform: s.Platform, Name: s.Name, Value: s.Value,
				Mode: mode, Requirements: s.Requirements})
		}
		if len(ac.Settings) > 0 {
			res.Applied = append(res.Applied, ac)
		}
	}
	return policy.SettingsByStep(res, maps)
}

var settingID = regexp.MustCompile(`\b[A-Z]{3}-\d{3}\b`)

// notReadDetail marks drift a step reports because it could not read or
// would not judge (a refused call, a missing group, an add-on), not
// because the tenant differs.
var notReadDetail = regexp.MustCompile(`(?i)not applied|permission|refused|forbidden|unauthori[sz]ed|\b40[13]\b|unresolved|no handler`)

type verifyOut struct {
	OK    bool `json:"ok"`
	Drift []struct {
		Action string `json:"action"`
		Target string `json:"target"`
		Detail string `json:"detail"`
	} `json:"drift"`
	Skipped []struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	} `json:"skipped"`
}

// ReadSettings turns the catalogue and the steps' verify results into one
// read per setting. Settings whose provider did not run, and settings a
// person checks (gates), unresolved, add-on or not-applicable settings, are
// "not read" with the reason; no value is assumed.
func ReadSettings(cat *policy.Catalogue, maps map[string]policy.Mapping, bySteps map[string][]policy.StepSetting, outcomes map[string]StepOutcome) map[string]*SettingRead {
	out := map[string]*SettingRead{}
	for _, c := range cat.Centres {
		for _, s := range c.Settings {
			m := maps[s.ID]
			sr := &SettingRead{ID: s.ID, Centre: c.ID, Name: s.Name, Expected: s.Value, Tier: s.Tier, Step: m.Step, Provider: m.Provider,
				Query: m.Read, Check: m.Check, Status: NotRead}
			switch m.Status {
			case "resolved":
				sr.Detail = "not run"
			case "gate":
				sr.Detail = "a person confirms this setting (a gate); read: " + m.Read
			case "not-applied":
				sr.Detail = fmt.Sprintf("needs a paid add-on (capability %s); not part of the subscription", s.Capability)
			case "not-applicable":
				sr.Detail = "not applicable: " + m.Notes
			default:
				sr.Detail = "no automated read yet: " + m.Notes
			}
			out[s.ID] = sr
		}
	}
	steps := make([]string, 0, len(bySteps))
	for st := range bySteps {
		steps = append(steps, st)
	}
	sort.Strings(steps)
	for _, st := range steps {
		ids := bySteps[st]
		o, ok := outcomes[st]
		if !ok || !o.Ran {
			reason := o.Reason
			if !ok || reason == "" {
				prov := ""
				if len(ids) > 0 {
					prov = maps[ids[0].ID].Provider
				}
				reason = "needs " + NeedsFor(prov, st)
				if GraphProviders[prov] && !ok {
					reason = "the policy step " + st + " is not built yet; nothing was read"
				}
			}
			for _, s := range ids {
				if sr := out[s.ID]; sr != nil {
					sr.Detail = reason
				}
			}
			continue
		}
		applyVerify(out, ids, o)
	}
	return out
}

func applyVerify(out map[string]*SettingRead, ids []policy.StepSetting, o StepOutcome) {
	var v verifyOut
	if err := json.Unmarshal(o.Verify, &v); err != nil {
		for _, s := range ids {
			if sr := out[s.ID]; sr != nil {
				sr.Detail = "the step's verify result could not be read: " + err.Error()
			}
		}
		return
	}
	mine := map[string]bool{}
	for _, s := range ids {
		mine[s.ID] = true
	}
	type res struct {
		status string
		detail []string
	}
	got := map[string]*res{}
	set := func(id, status, detail string) {
		r := got[id]
		if r == nil {
			r = &res{status: status}
			got[id] = r
		}
		// fail outranks not read for the same setting
		if status == Fail {
			r.status = Fail
		}
		r.detail = append(r.detail, detail)
	}
	for _, sk := range v.Skipped {
		if mine[sk.ID] {
			set(sk.ID, NotRead, "not read: "+sk.Reason)
		}
	}
	for _, w := range o.Warnings {
		for _, id := range settingID.FindAllString(w, -1) {
			if mine[id] {
				set(id, NotRead, "not read: "+w)
			}
		}
	}
	var loose []string
	for _, d := range v.Drift {
		text := strings.TrimSpace(d.Target + ": " + d.Detail)
		found := settingID.FindAllString(d.Target, -1)
		if len(found) == 0 {
			found = settingID.FindAllString(d.Detail, -1)
		}
		status := Fail
		if notReadDetail.MatchString(text) {
			status = NotRead
		}
		hit := false
		for _, id := range found {
			if mine[id] {
				set(id, status, text)
				hit = true
			}
		}
		if !hit {
			loose = append(loose, text)
		}
	}
	if !v.OK && len(v.Drift) == 0 && len(v.Skipped) == 0 {
		loose = append(loose, "verify did not pass and gave no detail")
	}
	for _, s := range ids {
		sr := out[s.ID]
		if sr == nil {
			continue
		}
		switch r := got[s.ID]; {
		case r != nil:
			sr.Status, sr.Detail = r.status, strings.Join(r.detail, "; ")
		case len(loose) > 0:
			sr.Status = NotRead
			sr.Detail = fmt.Sprintf("the step reported %d difference(s) not tied to one setting: %s", len(loose), strings.Join(loose, "; "))
		default:
			sr.Status, sr.Detail = Pass, "verified by "+o.Step
		}
	}
}
