package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Smohan-vcode/O365-Box/engine/internal/report"
)

// Mapping is one setting's resolution (T14-03): how it is applied, read
// back and monitored. Stored in expected-state/setting-ids/<centre>.json.
type Mapping struct {
	Status       string         `json:"status"` // resolved | gate | unresolved | not-applied
	Provider     string         `json:"provider"`
	Step         string         `json:"step"`
	Method       string         `json:"method"`
	Apply        string         `json:"apply"`
	Payload      any            `json:"payload"`
	PayloadAudit any            `json:"payload_audit,omitempty"`
	Read         string         `json:"read"`
	Check        string         `json:"check"`
	Permissions  []string       `json:"permissions"`
	Roles        []string       `json:"roles"`
	TCM          map[string]any `json:"tcm"`
	Verified     string         `json:"verified"`
	Notes        string         `json:"notes"`
	// Variants are the forms a choice can select (SPEC §24); each may
	// replace the payload and check. Absent fields keep the defaults.
	Variants map[string]Variant `json:"variants,omitempty"`
}

// Variant is one choice-selected form of a mapping.
type Variant struct {
	Payload any `json:"payload,omitempty"`
	// PayloadAudit is the variant's audit or report-only form; a staged
	// setting's variant with a payload needs one.
	PayloadAudit any    `json:"payload_audit,omitempty"`
	Check        string `json:"check,omitempty"`
	Notes        string `json:"notes"`
}

// Mapping statuses.
var mappingStatuses = map[string]bool{"resolved": true, "gate": true, "unresolved": true, "not-applied": true, "not-applicable": true}

// KnownProviders are the providers a mapping may name.
var KnownProviders = map[string]bool{"identity": true, "exchange": true, "sharepoint": true, "teams": true,
	"intune": true, "powerplatform": true, "dns": true, "purview": true}

// LoadMappings reads every expected-state/setting-ids/*.json file.
func LoadMappings(dir string) (map[string]Mapping, map[string]string, error) {
	out := map[string]Mapping{}
	file := map[string]string{}
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(paths)
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, nil, err
		}
		var m map[string]Mapping
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", p, err)
		}
		for id, v := range m {
			if prev, dup := file[id]; dup {
				return nil, nil, fmt.Errorf("setting %s is mapped in %s and %s", id, prev, filepath.Base(p))
			}
			out[id] = v
			file[id] = filepath.Base(p)
		}
	}
	return out, file, nil
}

// CheckMappings reports settings without a mapping, mappings for unknown
// settings, and malformed entries (T14-03 exit criteria).
func (c *Catalogue) CheckMappings(maps map[string]Mapping, file map[string]string) []report.Issue {
	var issues []report.Issue
	add := func(f, id, msg string) {
		issues = append(issues, report.Issue{Check: "mappings", File: f, Path: id, Message: msg})
	}
	_, settings, _ := c.IDs()
	known := map[string]bool{}
	for _, id := range settings {
		known[id] = true
		if _, ok := maps[id]; !ok {
			add("expected-state/setting-ids/"+c.Setting(id).Centre+".json", id, "setting has no mapping")
		}
	}
	ids := make([]string, 0, len(maps))
	for id := range maps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		m := maps[id]
		f := "expected-state/setting-ids/" + file[id]
		if !known[id] {
			add(f, id, "mapping for a setting that is not in the catalogue")
			continue
		}
		if !mappingStatuses[m.Status] {
			add(f, id, fmt.Sprintf("status %q is not resolved, gate, unresolved, not-applied or not-applicable", m.Status))
		}
		if m.Status == "not-applied" {
			if st := c.Setting(id); st != nil && st.Tier != "addon" {
				add(f, id, "only add-on settings may be not-applied")
			}
			continue
		}
		if m.Status == "resolved" {
			if !KnownProviders[m.Provider] {
				add(f, id, fmt.Sprintf("unknown provider %q", m.Provider))
			}
			if m.Apply == "" || m.Read == "" || m.Check == "" {
				add(f, id, "a resolved setting needs apply, read and check")
			}
			if st := c.Setting(id); st != nil && !strings.HasPrefix(m.Step+".", "policy."+st.Centre+".") {
				add(f, id, fmt.Sprintf("step %q is not policy.%s or policy.%s.<provider>", m.Step, st.Centre, st.Centre))
			}
			if st := c.Setting(id); st != nil && st.Mode == "staged" && m.PayloadAudit == nil {
				add(f, id, "a staged setting needs payload_audit (its audit or report-only form)")
			}
			for k, v := range m.Variants {
				if st := c.Setting(id); st != nil && st.Mode == "staged" && v.Payload != nil && v.PayloadAudit == nil {
					add(f, id, "variant "+k+" of a staged setting needs payload_audit")
				}
			}
		}
		if (m.Status == "gate" || m.Status == "unresolved" || m.Status == "not-applicable") && strings.TrimSpace(m.Notes) == "" {
			add(f, id, "a gate, unresolved or not-applicable setting needs notes saying why")
		}
	}
	report.Sort(issues)
	return issues
}

// CoverageMap renders expected-state/baseline-coverage.yaml (S-02): for
// every setting, how it is applied and how drift is monitored.
func (c *Catalogue) CoverageMap(maps map[string]Mapping) ([]byte, error) {
	_, settings, _ := c.IDs()
	sort.Strings(settings)
	type row struct {
		Setting   string `yaml:"setting"`
		Centre    string `yaml:"centre"`
		Status    string `yaml:"status"`
		ApplyBy   string `yaml:"apply_by"`
		MonitorBy string `yaml:"monitor_by"`
	}
	var rows []row
	for _, id := range settings {
		m, ok := maps[id]
		if !ok {
			continue
		}
		r := row{Setting: id, Centre: c.Setting(id).Centre, Status: m.Status}
		switch m.Status {
		case "resolved":
			r.ApplyBy = fmt.Sprintf("%s step %s (%s)", m.Provider, m.Step, m.Method)
			r.MonitorBy = "box drift (" + m.Step + " verify)"
			if res, _ := m.TCM["resource"].(string); res != "" {
				r.MonitorBy += "; configuration monitor " + res + " after SC-20"
			}
		case "gate":
			r.ApplyBy, r.MonitorBy = "gate (a person, guided)", "gate check"
		case "unresolved":
			r.ApplyBy, r.MonitorBy = "unresolved: decision needed", "none"
		case "not-applicable":
			r.ApplyBy, r.MonitorBy = "not applicable (owner decision)", "none"
		default:
			r.ApplyBy, r.MonitorBy = "not applied (add-on)", "none"
		}
		rows = append(rows, r)
	}
	head := "# Generated by `box policy mappings` from expected-state/setting-ids/*.json\n" +
		"# (T14-03, S-02). Do not edit by hand; CI runs `box policy mappings --check`.\n"
	b, err := yaml.Marshal(map[string]any{"settings": rows})
	if err != nil {
		return nil, err
	}
	return append([]byte(head), b...), nil
}

// StepSetting is one applied setting as a policy.<area> step receives it
// (T14-04): the payload to write is the variant's, else the audit form when
// the mode is audit or report and the mapping has one, else the payload.
type StepSetting struct {
	ID           string   `json:"id"`
	Centre       string   `json:"centre"`
	Name         string   `json:"name"`
	Value        string   `json:"value"`
	Mode         string   `json:"mode"`
	Variant      string   `json:"variant,omitempty"`
	Method       string   `json:"method"`
	Payload      any      `json:"payload"`
	Check        string   `json:"check"`
	Requirements []string `json:"requirements"`
}

// SettingsByStep groups the applied, resolved settings by mapping step.
func SettingsByStep(res *Result, maps map[string]Mapping) map[string][]StepSetting {
	out := map[string][]StepSetting{}
	for _, c := range res.Applied {
		for _, s := range c.Settings {
			m, ok := maps[s.ID]
			if !ok || m.Status != "resolved" || m.Step == "" {
				continue
			}
			payload, check := m.Payload, m.Check
			if (s.Mode == ModeAudit || s.Mode == ModeReport) && m.PayloadAudit != nil {
				payload = m.PayloadAudit
			}
			if v, ok := m.Variants[s.Variant]; ok {
				if v.Payload != nil {
					payload = v.Payload
				}
				if (s.Mode == ModeAudit || s.Mode == ModeReport) && v.PayloadAudit != nil {
					payload = v.PayloadAudit
				}
				if v.Check != "" {
					check = v.Check
				}
			}
			out[m.Step] = append(out[m.Step], StepSetting{ID: s.ID, Centre: c.ID, Name: s.Name, Value: s.Value, Mode: s.Mode,
				Variant: s.Variant, Method: m.Method, Payload: payload, Check: check, Requirements: s.Requirements})
		}
	}
	return out
}
