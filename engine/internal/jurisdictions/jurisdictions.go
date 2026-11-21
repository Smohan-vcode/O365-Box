// Package jurisdictions loads and checks the rules files
// defaults/jurisdictions/<code>.yaml (SPEC §19.1). The files hold rules
// only, never company values.
package jurisdictions

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FirstRelease lists the codes the first release ships (SPEC §19.1).
var FirstRelease = []string{
	"generic",
	"us", "us-de", "us-ca", "us-tx", "us-ny", "us-wa", "us-fl",
	"ca", "ca-on", "ca-qc", "ca-bc",
	"gb",
	"eu", "de", "fr", "ie", "nl", "es", "it",
	"ch",
	"au", "nz", "sg", "in", "jp", "hk",
	"ae", "br", "mx",
}

// Fields are the correspondence field names a rules file may use.
var Fields = []string{
	"legal_name", "legal_form", "place_of_registration", "registration_number",
	"register_name", "registered_office", "share_capital", "directors",
	"managing_directors", "supervisory_board_chair", "representative_name",
	"sole_shareholder", "liquidation_status", "vat_number", "tax_number",
	"postal_address", "telephone", "contact_email", "website", "unsubscribe",
}

// Notification is one duty to notify after an incident.
type Notification struct {
	Regime      string `yaml:"regime" json:"regime"`
	What        string `yaml:"what" json:"what"`
	To          string `yaml:"to" json:"to"`
	Deadline    string `yaml:"deadline" json:"deadline"`
	AppliesWhen string `yaml:"applies_when" json:"applies_when"`
}

// Correspondence is what must appear on letters, emails and websites.
type Correspondence struct {
	Required        []string          `yaml:"required" json:"required"`
	Conditional     map[string]string `yaml:"conditional" json:"conditional,omitempty"`
	Website         []string          `yaml:"website" json:"website,omitempty"`
	CommercialEmail []string          `yaml:"commercial_email" json:"commercial_email,omitempty"`
	Notes           string            `yaml:"notes" json:"notes,omitempty"`
}

// Privacy is the privacy regime of the place.
type Privacy struct {
	Regime                          string `yaml:"regime" json:"regime"`
	Regulator                       string `yaml:"regulator" json:"regulator"`
	RepresentativeRequiredIfOutside bool   `yaml:"representative_required_if_outside" json:"representative_required_if_outside"`
	Notes                           string `yaml:"notes" json:"notes,omitempty"`
}

// Rules is one rules file.
type Rules struct {
	Code                  string            `yaml:"code" json:"code"`
	Name                  string            `yaml:"name" json:"name"`
	Extends               string            `yaml:"extends" json:"extends,omitempty"`
	Status                string            `yaml:"status" json:"status"`
	ReviewedOn            string            `yaml:"reviewed_on" json:"reviewed_on,omitempty"`
	Sources               []string          `yaml:"sources" json:"sources"`
	Correspondence        Correspondence    `yaml:"correspondence" json:"correspondence"`
	Privacy               Privacy           `yaml:"privacy" json:"privacy"`
	Notices               []string          `yaml:"notices" json:"notices"`
	RetentionHints        map[string]string `yaml:"retention_hints" json:"retention_hints"`
	IncidentNotifications []Notification    `yaml:"incident_notifications" json:"incident_notifications"`
}

// Path is the rules file for code under the repository root.
func Path(root, code string) string {
	return filepath.Join(root, "defaults", "jurisdictions", code+".yaml")
}

// Load reads one rules file strictly: unknown keys are an error.
func Load(root, code string) (*Rules, error) {
	p := Path(root, code)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var r Rules
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&r); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return &r, nil
}

// Codes lists the codes that have a rules file, sorted.
func Codes(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "defaults", "jurisdictions"))
	if err != nil {
		return nil, err
	}
	var codes []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			codes = append(codes, strings.TrimSuffix(e.Name(), ".yaml"))
		}
	}
	sort.Strings(codes)
	return codes, nil
}

// Check loads every rules file and returns the problems found: a
// first-release code without a file, a file that does not parse, a code
// that differs from the file name, an extends naming no file, a status
// other than DRAFT or REVIEWED, a reviewed file without reviewed_on, an
// unknown correspondence field, or an incident entry missing a field.
func Check(root string) ([]string, error) {
	codes, err := Codes(root)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, c := range codes {
		have[c] = true
	}
	var issues []string
	add := func(format string, a ...any) { issues = append(issues, fmt.Sprintf(format, a...)) }
	for _, c := range FirstRelease {
		if !have[c] {
			add("%s: first-release jurisdiction has no rules file defaults/jurisdictions/%s.yaml", c, c)
		}
	}
	known := map[string]bool{}
	for _, f := range Fields {
		known[f] = true
	}
	for _, c := range codes {
		r, err := Load(root, c)
		if err != nil {
			add("%s: %v", c, err)
			continue
		}
		if r.Code != c {
			add("%s: code %q does not match the file name", c, r.Code)
		}
		if strings.TrimSpace(r.Name) == "" {
			add("%s: name is empty", c)
		}
		if r.Extends != "" && (!have[r.Extends] || r.Extends == c) {
			add("%s: extends %q, which has no rules file", c, r.Extends)
		}
		switch r.Status {
		case "DRAFT":
		case "REVIEWED":
			if r.ReviewedOn == "" {
				add("%s: status REVIEWED needs reviewed_on", c)
			}
		default:
			add("%s: status %q must be DRAFT or REVIEWED", c, r.Status)
		}
		if len(r.Sources) == 0 {
			add("%s: sources is empty", c)
		}
		fields := append(append(append([]string{}, r.Correspondence.Required...), r.Correspondence.Website...), r.Correspondence.CommercialEmail...)
		for f := range r.Correspondence.Conditional {
			fields = append(fields, f)
		}
		for _, f := range fields {
			if !known[f] {
				add("%s: unknown correspondence field %q", c, f)
			}
		}
		if strings.TrimSpace(r.Privacy.Regime) == "" || strings.TrimSpace(r.Privacy.Regulator) == "" {
			add("%s: privacy needs regime and regulator", c)
		}
		for i, n := range r.IncidentNotifications {
			for _, f := range []struct{ name, v string }{
				{"regime", n.Regime}, {"what", n.What}, {"to", n.To}, {"deadline", n.Deadline}, {"applies_when", n.AppliesWhen},
			} {
				if strings.TrimSpace(f.v) == "" {
					add("%s: incident_notifications[%d] has no %s", c, i, f.name)
				}
				if strings.ContainsAny(f.v, "|\n") {
					add("%s: incident_notifications[%d].%s must be one line without '|' (it is rendered into a table)", c, i, f.name)
				}
			}
		}
	}
	return issues, nil
}
