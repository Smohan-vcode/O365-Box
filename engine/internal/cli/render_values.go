package cli

// The company-wide values the policy templates are filled from
// (defaults/policies/PLACEHOLDERS.md). Every value comes from where it
// lives (R11): the configuration document, the Dictionary, the catalogue,
// the choices and the jurisdiction rules files. A value that is not there
// is left empty, with what the owner must supply, so the document shows a
// marker and stays a draft; nothing is invented.

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Smohan-vcode/O365-Box/engine/internal/choices"
	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
	"github.com/Smohan-vcode/O365-Box/engine/internal/docs"
	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
)

// resolvedPolicy resolves the configuration's policy selection for its plan.
func resolvedPolicy(root string, cfg *config.Config) (*policy.Catalogue, *policy.Result, policy.Selection, error) {
	cat, issues, err := loadCatalogue(root)
	if err != nil {
		return nil, nil, policy.Selection{}, err
	}
	if len(issues) > 0 {
		return nil, nil, policy.Selection{}, fmt.Errorf("policy catalogue has %d problem(s); run box policy validate", len(issues))
	}
	plans, err := enginePlans(root)
	if err != nil {
		return nil, nil, policy.Selection{}, err
	}
	caps := map[string]bool{}
	for _, c := range plans[cfg.Tenant.DefaultPlan] {
		caps[c] = true
	}
	sel, err := selection(root, cfg)
	if err != nil {
		return nil, nil, sel, err
	}
	res, err := cat.Resolve(sel, cfg.Tenant.DefaultPlan, caps)
	return cat, res, sel, err
}

// renderContext is what the templates are filled from. dictionary holds
// the Dictionary values by their key (company.address.street, ...): the
// tenant's list, or the --dictionary file before it exists.
func renderContext(root string, doc *config.Document, dictionary map[string]string) (docs.Context, error) {
	cat, res, sel, err := resolvedPolicy(root, &doc.Config)
	if err != nil {
		return docs.Context{}, err
	}
	known, err := docs.Placeholders(root)
	if err != nil {
		return docs.Context{}, err
	}
	cfg := &doc.Config
	values, pending := map[string]string{}, map[string]string{}
	// Dictionary: the configuration's company section, then the Dictionary.
	values = docs.Merge(values, docs.CompanyValues("dictionary.company", doc.Raw["company"]))
	for k, v := range dictionary {
		values = docs.Merge(values, map[string]string{"dictionary." + k: v})
	}
	if values["dictionary.company.registered_address"] == "" {
		var parts []string
		for _, k := range []string{"street", "city", "state", "postcode", "country"} {
			if v := strings.TrimSpace(dictionary["company.address."+k]); v != "" {
				parts = append(parts, v)
			}
		}
		if dictionary["company.address.street"] != "" {
			values["dictionary.company.registered_address"] = strings.Join(parts, ", ")
		}
	}
	// Role holders from the configuration's people (never the break-glass accounts).
	names, emails := map[string][]string{}, map[string][]string{}
	for _, u := range cfg.Users {
		if u.BreakGlass {
			continue
		}
		for _, r := range u.Roles {
			if n := strings.TrimSpace(u.GivenName + " " + u.FamilyName); n != "" {
				names[r] = append(names[r], n)
			}
			if u.Primary != "" && cfg.Company.PrimaryDomain != "" {
				emails[r] = append(emails[r], u.Primary+"@"+cfg.Company.PrimaryDomain)
			}
		}
	}
	for r, n := range names {
		values = docs.Merge(values, map[string]string{"dictionary.role." + r + ".name": strings.Join(n, ", ")})
	}
	for r, e := range emails {
		values = docs.Merge(values, map[string]string{"dictionary.role." + r + ".email": strings.Join(e, ", ")})
	}

	// Generated, company-wide.
	var fws []string
	for _, id := range cfg.Policy.Frameworks {
		if f := cat.Framework(id); f != nil && f.Title != "" {
			fws = append(fws, f.Title)
		} else {
			fws = append(fws, id)
		}
	}
	values["generated.frameworks"] = strings.Join(fws, ", ")
	if len(fws) == 0 {
		pending["generated.frameworks"] = "the frameworks the company follows (policy.frameworks in the configuration)"
	}
	for _, box := range []string{"security", "privacy"} {
		key := "generated.mailbox." + box
		for _, m := range cfg.Mail.Shared {
			if m.Name == box && cfg.Company.PrimaryDomain != "" {
				values[key] = box + "@" + cfg.Company.PrimaryDomain
			}
		}
		if values[key] == "" {
			pending[key] = "a " + box + " shared mailbox (mail.shared entry " + box + " in the configuration)"
		}
	}
	if s, why, err := retentionSchedule(root, doc.Raw["retention"]); err != nil {
		return docs.Context{}, err
	} else if s != "" {
		values["generated.retention_schedule"] = s
	} else {
		pending["generated.retention_schedule"] = why
	}
	if s, why, err := approvedAI(root, cfg, sel); err != nil {
		return docs.Context{}, err
	} else if s != "" {
		values["generated.approved_ai"] = s
	} else {
		pending["generated.approved_ai"] = why
	}
	jurisdictionValues(root, cfg, values, pending)
	return docs.Context{Values: values, Pending: pending, Known: known, Catalogue: cat, Resolved: res, Frameworks: cfg.Policy.Frameworks}, nil
}

// retentionSchedule is the schedule the owner confirmed: the configuration's
// retention section (classes it names override defaults/retention.yaml).
// Without a confirmed section it returns "" and the proposal to confirm.
func retentionSchedule(root string, raw any) (string, string, error) {
	var def struct {
		Classes map[string]struct {
			Keep string `yaml:"keep"`
		} `yaml:"classes"`
	}
	if err := readYAMLFile(filepath.Join(root, "defaults", "retention.yaml"), &def); err != nil {
		return "", "", err
	}
	keep := map[string]string{}
	for k, v := range def.Classes {
		keep[k] = v.Keep
	}
	cfg, _ := raw.(map[string]any)
	if classes, ok := cfg["classes"].(map[string]any); ok {
		for k, v := range classes {
			if m, ok := v.(map[string]any); ok {
				keep[k] = fmt.Sprint(m["keep"])
			} else if v != nil {
				keep[k] = fmt.Sprint(v)
			}
		}
	}
	classes := make([]string, 0, len(keep))
	for k := range keep {
		classes = append(classes, k)
	}
	sort.Strings(classes)
	if st, _ := cfg["status"].(string); cfg == nil || st != "CONFIRMED" {
		var p []string
		for _, k := range classes {
			p = append(p, strings.ReplaceAll(k, "_", " ")+" "+keep[k])
		}
		return "", "the owner's confirmation of the retention schedule (retention in the configuration, status CONFIRMED); proposed: " + strings.Join(p, "; "), nil
	}
	var b strings.Builder
	b.WriteString("| Record class | Keep for |\n| --- | --- |\n")
	for _, k := range classes {
		fmt.Fprintf(&b, "| %s | %s |\n", strings.ReplaceAll(k, "_", " "), keep[k])
	}
	return strings.TrimSuffix(b.String(), "\n"), "", nil
}

// approvedAI is the owner's answer to the AI choices and the ai_policy
// values. An answer the owner has not given (the recommended default) is
// not an approval: it returns "" and what to answer.
func approvedAI(root string, cfg *config.Config, sel policy.Selection) (string, string, error) {
	cc, err := choices.Load(filepath.Join(root, "defaults", "choices.yaml"))
	if err != nil {
		return "", "", err
	}
	var lines, open []string
	for _, ch := range cc.Choices {
		if !strings.HasPrefix(ch.ID, "ai.") && !strings.HasPrefix(ch.ID, "ai_policy.") {
			continue
		}
		v, ok := sel.Choices[ch.ID]
		if !ok {
			continue
		}
		label := v
		if o := ch.Option(v); o != nil {
			label = o.Label
		}
		if _, answered := cfg.Choices[ch.ID]; !answered || slices.Contains(sel.Defaulted, ch.ID) {
			open = append(open, fmt.Sprintf("%q (recommended: %s)", ch.Question, label))
			continue
		}
		lines = append(lines, "- "+ch.Question+": "+label)
	}
	var keys []string
	for k := range sel.Values {
		if strings.HasPrefix(k, "ai_policy.") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, "- "+strings.ReplaceAll(strings.TrimPrefix(k, "ai_policy."), "_", " ")+": "+sel.Values[k])
	}
	if len(open) > 0 {
		return "", "the owner's answer to " + strings.Join(open, ", "), nil
	}
	if len(lines) == 0 {
		return "", "the owner's answer to the AI choices in the onboarding", nil
	}
	return strings.Join(lines, "\n"), "", nil
}

// jurisdictionRules is the part of defaults/jurisdictions/<code>.yaml the
// policies read (SPEC §19.1).
type jurisdictionRules struct {
	Name                  string `yaml:"name"`
	Status                string `yaml:"status"`
	IncidentNotifications []struct {
		Regime   string `yaml:"regime"`
		What     string `yaml:"what"`
		To       string `yaml:"to"`
		Deadline string `yaml:"deadline"`
	} `yaml:"incident_notifications"`
}

func loadJurisdiction(root, code string) (*jurisdictionRules, error) {
	p := filepath.Join(root, "defaults", "jurisdictions", code+".yaml")
	if _, err := os.Stat(p); err != nil {
		return nil, err
	}
	var j jurisdictionRules
	return &j, readYAMLFile(p, &j)
}

// jurisdictionValues fills jurisdictions.* from the rules files the
// configuration selects; a missing file leaves the value to be supplied.
func jurisdictionValues(root string, cfg *config.Config, values, pending map[string]string) {
	name := func(code string) (string, *jurisdictionRules) {
		j, err := loadJurisdiction(root, code)
		if err != nil || strings.TrimSpace(j.Name) == "" {
			return "", nil
		}
		n := j.Name
		if j.Status != "REVIEWED" {
			n += " (rules not yet reviewed by counsel)"
		}
		return n, j
	}
	need := func(code string) string {
		return "the jurisdiction rules file defaults/jurisdictions/" + code + ".yaml with its name"
	}
	reg := cfg.Jurisdictions.Registered
	if n, _ := name(reg); n != "" {
		values["jurisdictions.registered"] = n
	} else if reg != "" {
		pending["jurisdictions.registered"] = need(reg)
	} else {
		pending["jurisdictions.registered"] = "where the company is registered (jurisdictions.registered in the configuration)"
	}
	var ops, missing []string
	var rows []string
	codes := append([]string{}, cfg.Jurisdictions.Operating...)
	if reg != "" && !slices.Contains(codes, reg) {
		codes = append([]string{reg}, codes...)
	}
	for _, c := range codes {
		n, j := name(c)
		if j == nil {
			missing = append(missing, c)
			continue
		}
		if slices.Contains(cfg.Jurisdictions.Operating, c) {
			ops = append(ops, n)
		}
		for _, x := range j.IncidentNotifications {
			rows = append(rows, fmt.Sprintf("| %s | %s | %s | %s | %s |", j.Name, x.Regime, x.What, x.To, x.Deadline))
		}
	}
	files := func() string {
		var l []string
		for _, c := range missing {
			l = append(l, "defaults/jurisdictions/"+c+".yaml")
		}
		return strings.Join(l, ", ")
	}
	if len(missing) == 0 && len(ops) > 0 {
		values["jurisdictions.operating"] = strings.Join(ops, ", ")
	} else if len(missing) > 0 {
		pending["jurisdictions.operating"] = "the jurisdiction rules files " + files()
	} else {
		pending["jurisdictions.operating"] = "where the company has customers, staff or data (jurisdictions.operating in the configuration)"
	}
	if len(missing) == 0 && len(rows) > 0 {
		values["jurisdictions.notification_deadlines"] = "| Jurisdiction | Regime | What | To whom | Deadline |\n| --- | --- | --- | --- | --- |\n" + strings.Join(rows, "\n")
	} else {
		what := "incident notification entries in the jurisdiction rules files"
		if len(missing) > 0 {
			what += " (missing: " + files() + ")"
		}
		pending["jurisdictions.notification_deadlines"] = what
	}
}
