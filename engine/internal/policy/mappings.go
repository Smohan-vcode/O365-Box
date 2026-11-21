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
}

// Mapping statuses.
var mappingStatuses = map[string]bool{"resolved": true, "gate": true, "unresolved": true, "not-applied": true}

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
			add(f, id, fmt.Sprintf("status %q is not resolved, gate, unresolved or not-applied", m.Status))
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
			if st := c.Setting(id); st != nil && st.Mode == "staged" && m.PayloadAudit == nil {
				add(f, id, "a staged setting needs payload_audit (its audit or report-only form)")
			}
		}
		if (m.Status == "gate" || m.Status == "unresolved") && strings.TrimSpace(m.Notes) == "" {
			add(f, id, "a gate or unresolved setting needs notes saying why")
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
