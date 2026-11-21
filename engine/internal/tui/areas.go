package tui

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Smohan-vcode/O365-Box/engine/internal/draft"
)

// Area is one entry of defaults/areas.yaml (SPEC §25.2): what an owner
// cares about, with the engine steps behind it.
type Area struct {
	ID    string   `yaml:"id"`
	Title string   `yaml:"title"`
	Plain string   `yaml:"plain"`
	Steps []string `yaml:"steps"`
}

// LoadAreas reads defaults/areas.yaml.
func LoadAreas(path string) ([]Area, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Areas []Area `yaml:"areas"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc.Areas, nil
}

// Selection is what the owner has answered so far, read from the draft:
// the inputs a plan by area needs.
type Selection struct {
	Tier    string          // policy.tier
	Modules map[string]bool // modules by name
	Choices map[string]string
}

// SelectionOf reads the selection from the draft. Modules the owner has
// not switched keep their default.
func SelectionOf(d *draft.Draft) Selection {
	s := Selection{Tier: d.String("policy.tier"), Modules: map[string]bool{}, Choices: map[string]string{}}
	for _, sw := range runSwitches {
		if sw.module != "" {
			s.Modules[sw.module] = switchOn(d, sw)
		}
	}
	s.Modules["policies"] = true
	for k, v := range d.Answers {
		if id, ok := strings.CutPrefix(k, "choices."); ok {
			if str, ok := v.(string); ok && str != "" {
				s.Choices[id] = str
			}
		}
	}
	return s
}

// AreaInfo is what the plan holds for one area, for its Review card.
type AreaInfo struct {
	Settings int      // settings the area applies with these answers
	Lines    []string // plain-word lines for the card
	Details  []string // what "Show details" lists, such as setting names
	Cost     string   // cost note; empty means none
	Off      string   // why nothing is set up here (the area needs no decision)
}

// AreaStatus is one area on the home screen, from the last check.
type AreaStatus struct {
	Area       string // area id
	Status     string // protected | watching | needs-you
	Note       string // plain words, such as "audit ends in 9 days"
	CanEnforce bool   // the audit period is clean: Enforce is offered
}

// Approval decisions stored in the draft as "approval.<area>".
const (
	Approved = "approved"
	NotNow   = "not-now"
)

func approvalKey(area string) string { return "approval." + area }

// Approvals lists the decisions in the draft by area id.
func Approvals(d *draft.Draft) map[string]string {
	out := map[string]string{}
	for k := range d.Answers {
		if id, ok := strings.CutPrefix(k, "approval."); ok {
			out[id] = d.String(k)
		}
	}
	return out
}
