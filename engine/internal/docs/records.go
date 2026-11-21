package docs

// Records: the recurring control tasks (F17, T10-02) that prove a written
// procedure is followed. Documents name them in their front matter
// (records: ["ControlTasks:access_review quarterly", ...]); the control
// set in defaults/controls.yaml defines each kind once, with its control,
// frequency, requirements, policy and procedure. Schedule joins the two;
// the sharepoint.control-tasks step keeps the next due ControlTasks item
// per kind, and the audit reads which kinds prove which requirement
// (RequirementRecords).

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
)

// ControlsFile is the control set, relative to the repository root.
const ControlsFile = "defaults/controls.yaml"

// Frequencies of a control or a control task.
const (
	Daily        = "daily" // each working day, Monday to Friday
	Weekly       = "weekly"
	Monthly      = "monthly"
	Quarterly    = "quarterly"
	Semiannually = "semiannually"
	Annually     = "annually"
	OnEvent      = "on_event"
)

// Frequencies lists every frequency, shortest period first.
var Frequencies = []string{Daily, Weekly, Monthly, Quarterly, Semiannually, Annually, OnEvent}

// frequencyWords maps the words documents use in their records to a
// frequency.
var frequencyWords = map[string]string{
	"each working day": Daily, "daily": Daily,
	"weekly": Weekly, "each week": Weekly,
	"monthly": Monthly, "each month": Monthly,
	"quarterly": Quarterly, "each quarter": Quarterly,
	"every six months": Semiannually, "twice a year": Semiannually, "semiannually": Semiannually,
	"yearly": Annually, "annually": Annually, "each year": Annually,
	"on event": OnEvent, "on_event": OnEvent,
}

// Artefacts the box can prepare from a read for a control task.
const (
	PrepareAccessReview = "access_review_sheet"
	PrepareAppConsents  = "app_consent_list"
)

var preparers = map[string]bool{PrepareAccessReview: true, PrepareAppConsents: true}

// Control is one entry of defaults/controls.yaml.
type Control struct {
	ID           string   `yaml:"id" json:"id"`
	Statement    string   `yaml:"statement" json:"statement"`
	SOC2         []string `yaml:"soc2" json:"soc2"`
	Frequency    string   `yaml:"frequency" json:"frequency"`
	Source       string   `yaml:"source" json:"source"`
	Automated    bool     `yaml:"automated" json:"automated"`
	Policy       string   `yaml:"policy" json:"policy"`
	Procedure    string   `yaml:"procedure,omitempty" json:"procedure,omitempty"`
	Requirements []string `yaml:"requirements,omitempty" json:"requirements,omitempty"`
	Prepares     string   `yaml:"prepares,omitempty" json:"prepares,omitempty"`
}

// TaskKind is the control task kind of a control whose source is
// task.<kind>, or "".
func (c Control) TaskKind() string {
	k, _ := strings.CutPrefix(c.Source, "task.")
	if k == c.Source {
		return ""
	}
	return k
}

// LoadControls reads defaults/controls.yaml under the repository root.
func LoadControls(root string) ([]Control, error) {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ControlsFile)))
	if err != nil {
		return nil, err
	}
	var f struct {
		Controls []Control `yaml:"controls"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", ControlsFile, err)
	}
	return f.Controls, nil
}

// Record is one entry of a document's records, parsed:
// "ControlTasks:access_review quarterly" -> {ControlTasks, access_review, quarterly}.
// For lists other than ControlTasks, Kind is the rest of the entry and
// Frequency is "".
type Record struct {
	List      string
	Kind      string
	Frequency string // a frequency constant, or "" when the words are not recognised
	Words     string // the frequency as written
}

// ParseRecord parses one records entry.
func ParseRecord(s string) Record {
	list, rest, _ := strings.Cut(strings.TrimSpace(s), ":")
	r := Record{List: list, Kind: rest}
	if list != "ControlTasks" {
		return r
	}
	kind, words, _ := strings.Cut(rest, " ")
	r.Kind, r.Words = kind, strings.TrimSpace(words)
	r.Frequency = frequencyWords[strings.ToLower(r.Words)]
	return r
}

// RecordKinds returns the control task kinds a document's records name,
// in order, without repeats.
func RecordKinds(d Doc) []Record {
	var out []Record
	seen := map[string]bool{}
	for _, s := range d.Records {
		r := ParseRecord(s)
		if r.List != "ControlTasks" || seen[r.Kind] {
			continue
		}
		seen[r.Kind] = true
		out = append(out, r)
	}
	return out
}

// TaskKind is one kind of recurring control task, as scheduled.
type TaskKind struct {
	Kind         string   `json:"kind"`
	ControlID    string   `json:"control_id"`
	Statement    string   `json:"statement"`
	SOC2         []string `json:"soc2"`
	Frequency    string   `json:"frequency"`
	OwnerRole    string   `json:"owner_role"`
	Policy       string   `json:"policy"`
	Procedure    string   `json:"procedure,omitempty"`
	Requirements []string `json:"requirements"`
	Documents    []string `json:"documents"` // every document whose records name the kind
	Prepares     string   `json:"prepares,omitempty"`
}

// Scheduled reports whether the kind has due dates (it is not on_event).
func (k TaskKind) Scheduled() bool { return k.Frequency != OnEvent }

// Schedule joins the control set with the documents' records. It returns
// the task kinds sorted by kind, and one message per problem: a kind a
// document names that no control defines, a frequency that differs, an
// unknown frequency or preparer, a control task no document names.
func Schedule(docs []Doc, controls []Control) ([]TaskKind, []string) {
	var problems []string
	byID := map[string]Doc{}
	for _, d := range docs {
		byID[d.ID] = d
	}
	kinds := map[string]*TaskKind{}
	for _, c := range controls {
		k := c.TaskKind()
		if !contains(Frequencies, c.Frequency) {
			problems = append(problems, fmt.Sprintf("%s: frequency %q is not one of %s", c.ID, c.Frequency, strings.Join(Frequencies, ", ")))
		}
		if k == "" {
			continue
		}
		if prev, dup := kinds[k]; dup {
			problems = append(problems, fmt.Sprintf("%s: task kind %s is already defined by %s", c.ID, k, prev.ControlID))
			continue
		}
		if c.Prepares != "" && !preparers[c.Prepares] {
			problems = append(problems, fmt.Sprintf("%s: prepares %q is not %s or %s", c.ID, c.Prepares, PrepareAccessReview, PrepareAppConsents))
		}
		owner := ""
		for _, ref := range []struct{ key, id, kind string }{{"policy", c.Policy, KindPolicy}, {"procedure", c.Procedure, KindProcedure}} {
			if ref.id == "" {
				if ref.key == "policy" {
					problems = append(problems, fmt.Sprintf("%s: a control task names its policy", c.ID))
				}
				continue
			}
			d, ok := byID[ref.id]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: %s %s is not in %s", c.ID, ref.key, ref.id, IndexFile))
				continue
			}
			if d.Kind != ref.kind {
				problems = append(problems, fmt.Sprintf("%s: %s %s is a %s", c.ID, ref.key, ref.id, d.Kind))
			}
			owner = d.OwnerRole // the procedure, when there is one, wins
		}
		kinds[k] = &TaskKind{Kind: k, ControlID: c.ID, Statement: c.Statement, SOC2: c.SOC2, Frequency: c.Frequency, OwnerRole: owner,
			Policy: c.Policy, Procedure: c.Procedure, Requirements: c.Requirements, Prepares: c.Prepares}
	}
	for _, d := range docs {
		for _, r := range RecordKinds(d) {
			k, ok := kinds[r.Kind]
			switch {
			case !ok:
				problems = append(problems, fmt.Sprintf("%s: records ControlTasks:%s, which no control in %s defines (source: task.%s)", d.ID, r.Kind, ControlsFile, r.Kind))
				continue
			case r.Frequency == "":
				problems = append(problems, fmt.Sprintf("%s: records ControlTasks:%s with frequency %q, which is not recognised", d.ID, r.Kind, r.Words))
			case r.Frequency != k.Frequency:
				problems = append(problems, fmt.Sprintf("%s: records ControlTasks:%s %s, but %s is %s", d.ID, r.Kind, r.Words, k.ControlID, k.Frequency))
			}
			k.Documents = append(k.Documents, d.ID)
		}
	}
	out := make([]TaskKind, 0, len(kinds))
	for _, k := range kinds {
		if len(k.Documents) == 0 {
			problems = append(problems, fmt.Sprintf("%s: task kind %s is named in no document's records", k.ControlID, k.Kind))
		}
		if k.Procedure != "" && !contains(k.Documents, k.Procedure) {
			problems = append(problems, fmt.Sprintf("%s: procedure %s does not record ControlTasks:%s", k.ControlID, k.Procedure, k.Kind))
		}
		out = append(out, *k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out, problems
}

// CheckRequirementRecords reports a requirement whose evidence is
// task.<kind>, or whose procedures name control-task:<kind>, that the
// kind's control does not list, and a listed requirement that is not in
// the catalogue. evidence and procedures are keyed by requirement id.
func CheckRequirementRecords(kinds []TaskKind, evidence map[string]string, procedures map[string][]string) []string {
	var out []string
	byKind := map[string]TaskKind{}
	for _, k := range kinds {
		byKind[k.Kind] = k
		for _, r := range k.Requirements {
			if _, ok := evidence[r]; !ok {
				out = append(out, fmt.Sprintf("%s: unknown requirement %s", k.ControlID, r))
			}
		}
	}
	need := func(req, kind, via string) {
		k, ok := byKind[kind]
		if !ok {
			out = append(out, fmt.Sprintf("%s: %s names task kind %s, which no control defines", req, via, kind))
		} else if !contains(k.Requirements, req) {
			out = append(out, fmt.Sprintf("%s: %s names %s, but %s does not list %s", req, via, kind, k.ControlID, req))
		}
	}
	ids := make([]string, 0, len(evidence))
	for id := range evidence {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if k, ok := strings.CutPrefix(evidence[id], "task."); ok {
			need(id, k, "evidence")
		}
		for _, p := range procedures[id] {
			if k, ok := strings.CutPrefix(p, "control-task:"); ok {
				need(id, k, "procedures")
			}
		}
	}
	return out
}

// LoadSchedule loads the documents, the control set and the requirement
// catalogue under the repository root and returns the task kinds with
// every problem Schedule and CheckRequirementRecords find. It returns an
// error only when a file cannot be read.
func LoadSchedule(root string) ([]TaskKind, []string, error) {
	docs, err := Load(root)
	if err != nil {
		return nil, nil, err
	}
	controls, err := LoadControls(root)
	if err != nil {
		return nil, nil, err
	}
	cat, err := policy.Load(filepath.Join(root, "policy"), "policy")
	if err != nil {
		return nil, nil, err
	}
	kinds, problems := Schedule(docs, controls)
	ev, pr := map[string]string{}, map[string][]string{}
	for _, r := range cat.Requirements {
		ev[r.ID], pr[r.ID] = r.Evidence, r.Procedures
	}
	problems = append(problems, CheckRequirementRecords(kinds, ev, pr)...)
	return kinds, problems, nil
}

// RequirementRecords maps each requirement id to the control task kinds
// whose completed records prove it, with their frequency. The audit reads
// ControlTasks and Evidence for these kinds to show that a procedure was
// followed in the audit period.
func RequirementRecords(kinds []TaskKind) map[string][]TaskKind {
	out := map[string][]TaskKind{}
	for _, k := range kinds {
		for _, r := range k.Requirements {
			out[r] = append(out[r], k)
		}
	}
	return out
}

// Period is one occurrence of a control task.
type Period struct {
	ID     string `json:"period"`  // 2026-11-23, 2026-W48, 2026-11, 2026-Q4, 2026-H2, 2026
	TaskID string `json:"task_id"` // <kind>-<period>
	Start  string `json:"start"`   // first day, yyyy-mm-dd
	DueOn  string `json:"due_on"`  // last day, yyyy-mm-dd: the task is due by the end of its period
}

const day = "2006-01-02"

// PeriodOf returns the period of a scheduled frequency that holds the
// date t (its calendar date is used, in t's location). For daily, a
// Saturday or Sunday belongs to the next Monday.
func PeriodOf(kind, freq string, t time.Time) (Period, error) {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	var id string
	var start, end time.Time
	switch freq {
	case Daily:
		for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			d = d.AddDate(0, 0, 1)
		}
		start, end, id = d, d, d.Format(day)
	case Weekly:
		off := (int(d.Weekday()) + 6) % 7 // Monday = 0
		start = d.AddDate(0, 0, -off)
		end = start.AddDate(0, 0, 6)
		y, w := start.ISOWeek()
		id = fmt.Sprintf("%d-W%02d", y, w)
	case Monthly:
		start = time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 1, -1)
		id = start.Format("2006-01")
	case Quarterly:
		q := (int(d.Month()) - 1) / 3
		start = time.Date(d.Year(), time.Month(q*3+1), 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 3, -1)
		id = fmt.Sprintf("%d-Q%d", d.Year(), q+1)
	case Semiannually:
		h := (int(d.Month()) - 1) / 6
		start = time.Date(d.Year(), time.Month(h*6+1), 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 6, -1)
		id = fmt.Sprintf("%d-H%d", d.Year(), h+1)
	case Annually:
		start = time.Date(d.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(1, 0, -1)
		id = fmt.Sprintf("%d", d.Year())
	default:
		return Period{}, fmt.Errorf("frequency %q has no periods", freq)
	}
	return Period{ID: id, TaskID: kind + "-" + id, Start: start.Format(day), DueOn: end.Format(day)}, nil
}

// NextPeriod returns the period after p.
func NextPeriod(kind, freq string, p Period) (Period, error) {
	end, err := time.Parse(day, p.DueOn)
	if err != nil {
		return Period{}, err
	}
	return PeriodOf(kind, freq, end.AddDate(0, 0, 1))
}

// Due is one scheduled kind with its current and next period, the input of
// the sharepoint.control-tasks step.
type Due struct {
	TaskKind
	Current Period `json:"current"`
	Next    Period `json:"next"`
}

// DueOn returns every scheduled kind with its period holding today and the
// one after it. On-event kinds are left out.
func DueOn(kinds []TaskKind, today time.Time) ([]Due, error) {
	var out []Due
	for _, k := range kinds {
		if !k.Scheduled() {
			continue
		}
		cur, err := PeriodOf(k.Kind, k.Frequency, today)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k.Kind, err)
		}
		next, err := NextPeriod(k.Kind, k.Frequency, cur)
		if err != nil {
			return nil, err
		}
		out = append(out, Due{TaskKind: k, Current: cur, Next: next})
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
