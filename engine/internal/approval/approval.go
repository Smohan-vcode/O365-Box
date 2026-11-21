// Package approval keeps a tenant's proposal and the owner's approvals
// (SPEC §25.1): the box suggests, the owner decides. A proposal lists every
// planned change in plain words, grouped by area (§25.2); an approval
// records who approved which change digests and when; a rejection is an
// exception with the owner's reason. The executor applies a step only when
// every one of its planned changes is approved.
package approval

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Smohan-vcode/O365-Box/engine/internal/plan"
	"github.com/Smohan-vcode/O365-Box/engine/internal/run"
)

// Other is the area of a step that defaults/areas.yaml does not map.
const Other = "other"

// Area is one entry of defaults/areas.yaml.
type Area struct {
	ID     string   `yaml:"id" json:"id"`
	Title  string   `yaml:"title" json:"title"`
	Plain  string   `yaml:"plain" json:"plain"`
	Behind []string `yaml:"behind" json:"behind,omitempty"`
	Steps  []string `yaml:"steps" json:"steps"`
}

// Areas is defaults/areas.yaml.
type Areas struct {
	Areas []Area `yaml:"areas"`
}

// LoadAreas reads an areas file.
func LoadAreas(path string) (*Areas, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var a Areas
	if err := yaml.Unmarshal(b, &a); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	seen := map[string]bool{}
	for _, x := range a.Areas {
		if x.ID == "" || x.Title == "" || x.Plain == "" {
			return nil, fmt.Errorf("%s: every area needs id, title and plain", path)
		}
		if seen[x.ID] || x.ID == Other {
			return nil, fmt.Errorf("%s: area id %q is used twice or reserved", path, x.ID)
		}
		seen[x.ID] = true
	}
	return &a, nil
}

// matches reports whether pattern covers step: the same id, or a prefix
// ending in "." or "-".
func matches(pattern, step string) bool {
	if pattern == step {
		return true
	}
	return (strings.HasSuffix(pattern, ".") || strings.HasSuffix(pattern, "-")) && strings.HasPrefix(step, pattern)
}

// Of returns the area of a step: the area of the longest matching entry,
// or Other.
func (a *Areas) Of(step string) string {
	best, area := -1, Other
	for _, x := range a.Areas {
		for _, p := range x.Steps {
			if matches(p, step) && len(p) > best {
				best, area = len(p), x.ID
			}
		}
	}
	return area
}

// Get returns an area by id, or nil.
func (a *Areas) Get(id string) *Area {
	for i := range a.Areas {
		if a.Areas[i].ID == id {
			return &a.Areas[i]
		}
	}
	return nil
}

// Title returns an area's title; Other is "Other".
func (a *Areas) Title(id string) string {
	if x := a.Get(id); x != nil {
		return x.Title
	}
	return "Other"
}

// Change is one planned change in a proposal.
type Change struct {
	Digest    string `json:"digest"`
	Area      string `json:"area"`
	Step      string `json:"step"`
	StepTitle string `json:"step_title,omitempty"`
	Action    string `json:"action"`
	Target    string `json:"target"`
	Detail    string `json:"detail,omitempty"`
	Text      string `json:"text"` // the change in plain words
}

// Run returns the change as the executor sees it.
func (c Change) Run() run.Change {
	return run.Change{Action: c.Action, Target: c.Target, Detail: c.Detail}
}

// NewChange describes a step's planned change for the proposal.
func NewChange(areas *Areas, step *plan.Step, c run.Change) Change {
	return Change{Digest: run.Digest(step.ID, c), Area: areas.Of(step.ID), Step: step.ID, StepTitle: step.Title,
		Action: c.Action, Target: c.Target, Detail: c.Detail, Text: Plain(c)}
}

var verbs = map[string]string{"create": "Create", "update": "Change", "delete": "Remove", "repair": "Repair"}

// Plain is a change in plain words.
func Plain(c run.Change) string {
	v, ok := verbs[strings.ToLower(c.Action)]
	if !ok && c.Action != "" {
		v = strings.ToUpper(c.Action[:1]) + c.Action[1:]
	}
	s := v + " " + c.Target
	if c.Detail != "" {
		s += ": " + c.Detail
	}
	return s
}

// Proposal is every change a plan found, for the owner to approve.
type Proposal struct {
	Version int       `json:"version"`
	Tenant  string    `json:"tenant"`
	Run     string    `json:"run"`
	Created time.Time `json:"created"`
	Changes []Change  `json:"changes"`
}

// Group is one area of a proposal.
type Group struct {
	Area    string   `json:"area"`
	Title   string   `json:"title"`
	Changes []Change `json:"changes"`
}

// ByArea groups changes in the order of the areas file, Other last.
func ByArea(areas *Areas, changes []Change) []Group {
	by := map[string][]Change{}
	for _, c := range changes {
		by[c.Area] = append(by[c.Area], c)
	}
	var out []Group
	for _, a := range append(append([]Area{}, areas.Areas...), Area{ID: Other}) {
		if cs := by[a.ID]; len(cs) > 0 {
			out = append(out, Group{Area: a.ID, Title: areas.Title(a.ID), Changes: cs})
		}
	}
	return out
}

// Sort orders changes by step, then target and action.
func Sort(cs []Change) {
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Step != cs[j].Step {
			return cs[i].Step < cs[j].Step
		}
		if cs[i].Target != cs[j].Target {
			return cs[i].Target < cs[j].Target
		}
		return cs[i].Action < cs[j].Action
	})
}

// Dir is where a tenant's proposal and approvals live: next to the state
// file when one is given, else the user's config directory per tenant
// (beside the pipeline record).
func Dir(tenant, statePath string) (string, error) {
	if statePath != "" {
		return filepath.Dir(statePath), nil
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if tenant == "" {
		tenant = "unknown-tenant"
	}
	return filepath.Join(d, "box", "tenants", tenant), nil
}

// File names inside Dir.
const (
	ProposalFile  = "proposal.json"
	ApprovalsFile = "approvals.json"
	EventsFile    = "events.jsonl" // Events items not yet in the tenant's Events list
)

// WriteProposal writes p to path atomically.
func WriteProposal(path string, p *Proposal) error {
	p.Version = 1
	return writeJSON(path, p)
}

// ErrNoProposal means box plan --proposal has not run for the tenant.
var ErrNoProposal = errors.New("no proposal for this tenant: run box plan --proposal first")

// ReadProposal reads a proposal.
func ReadProposal(path string) (*Proposal, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoProposal
	}
	if err != nil {
		return nil, err
	}
	var p Proposal
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &p, nil
}

// Record is one approved change. An approval authorises one apply of that
// exact change: once applied it is kept as history and a change that comes
// back (drift, a new plan) needs a new approval.
type Record struct {
	Digest     string     `json:"digest"`
	Step       string     `json:"step"`
	Area       string     `json:"area"`
	Action     string     `json:"action"`
	Target     string     `json:"target"`
	Detail     string     `json:"detail,omitempty"`
	By         string     `json:"by"`
	At         time.Time  `json:"at"`
	Proposal   string     `json:"proposal,omitempty"` // run that wrote the proposal
	AppliedAt  *time.Time `json:"applied_at,omitempty"`
	AppliedRun string     `json:"applied_run,omitempty"`
}

// Exception is a rejected change: the owner decided it is not applied.
type Exception struct {
	Digest   string    `json:"digest"`
	Step     string    `json:"step"`
	Area     string    `json:"area"`
	Action   string    `json:"action"`
	Target   string    `json:"target"`
	Detail   string    `json:"detail,omitempty"`
	Reason   string    `json:"reason"`
	By       string    `json:"by"`
	At       time.Time `json:"at"`
	Proposal string    `json:"proposal,omitempty"`
}

// File is a tenant's approvals, backed by one JSON file. It implements
// run.Approvals.
type File struct {
	mu   sync.Mutex
	path string
	data struct {
		Version    int         `json:"version"`
		Approvals  []Record    `json:"approvals"`
		Exceptions []Exception `json:"exceptions"`
	}
}

// Open loads path, or starts empty if it does not exist.
func Open(path string) (*File, error) {
	f := &File{path: path}
	f.data.Version = 1
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &f.data); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// Approvals returns a copy of every approval record.
func (f *File) Approvals() []Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Record(nil), f.data.Approvals...)
}

// Exceptions returns a copy of every exception.
func (f *File) Exceptions() []Exception {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Exception(nil), f.data.Exceptions...)
}

func (f *File) pending(digest string) bool {
	for _, r := range f.data.Approvals {
		if r.Digest == digest && r.AppliedAt == nil {
			return true
		}
	}
	return false
}

func (f *File) exception(digest string) bool {
	for _, e := range f.data.Exceptions {
		if e.Digest == digest {
			return true
		}
	}
	return false
}

// Decision is a change's state: "approved" (not yet applied), "rejected"
// or "" (needs approval).
func (f *File) Decision(digest string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case f.exception(digest):
		return "rejected"
	case f.pending(digest):
		return "approved"
	}
	return ""
}

// Approve records approvals for changes not already approved. Approving a
// change the owner rejected earlier lifts that exception. It returns the
// changes newly approved.
func (f *File) Approve(changes []Change, by, proposal string, at time.Time) ([]Change, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var added []Change
	for _, c := range changes {
		if f.pending(c.Digest) {
			continue
		}
		f.dropException(c.Digest)
		f.data.Approvals = append(f.data.Approvals, Record{Digest: c.Digest, Step: c.Step, Area: c.Area, Action: c.Action,
			Target: c.Target, Detail: c.Detail, By: by, At: at, Proposal: proposal})
		added = append(added, c)
	}
	return added, f.write()
}

// Reject records each change as an exception with the reason, replacing
// any approval of it that was not applied yet.
func (f *File) Reject(changes []Change, reason, by, proposal string, at time.Time) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("a rejection needs a reason")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range changes {
		keep := f.data.Approvals[:0]
		for _, r := range f.data.Approvals {
			if r.Digest != c.Digest || r.AppliedAt != nil {
				keep = append(keep, r)
			}
		}
		f.data.Approvals = keep
		f.dropException(c.Digest)
		f.data.Exceptions = append(f.data.Exceptions, Exception{Digest: c.Digest, Step: c.Step, Area: c.Area, Action: c.Action,
			Target: c.Target, Detail: c.Detail, Reason: reason, By: by, At: at, Proposal: proposal})
	}
	return f.write()
}

func (f *File) dropException(digest string) {
	keep := f.data.Exceptions[:0]
	for _, e := range f.data.Exceptions {
		if e.Digest != digest {
			keep = append(keep, e)
		}
	}
	f.data.Exceptions = keep
}

// Check implements run.Approvals.
func (f *File) Check(step string, changes []run.Change) (unapproved, rejected []run.Change) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range changes {
		d := run.Digest(step, c)
		switch {
		case f.exception(d):
			rejected = append(rejected, c)
		case !f.pending(d):
			unapproved = append(unapproved, c)
		}
	}
	return unapproved, rejected
}

// Applied implements run.Approvals: the approvals of the applied changes
// become history.
func (f *File) Applied(step string, changes []run.Change, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	for _, c := range changes {
		d := run.Digest(step, c)
		for i := range f.data.Approvals {
			if r := &f.data.Approvals[i]; r.Digest == d && r.AppliedAt == nil {
				r.AppliedAt, r.AppliedRun = &now, runID
			}
		}
	}
	return f.write()
}

func (f *File) write() error { return writeJSON(f.path, &f.data) }

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// EventItem is one item of the Events list (engine/register/lists.yaml):
// append-only (R10). It carries digests, never change targets, so no
// person's details reach it.
type EventItem struct {
	EventID   string         `json:"event_id"`
	At        time.Time      `json:"at"`
	Actor     string         `json:"actor"`
	Entity    string         `json:"entity"`
	EntityID  string         `json:"entity_id"`
	Action    string         `json:"action"`
	FromState string         `json:"from_state,omitempty"`
	ToState   string         `json:"to_state,omitempty"`
	Detail    map[string]any `json:"detail,omitempty"`
}

// AppendEvent adds an item to the local Events file (one JSON line each);
// items are only ever added.
func AppendEvent(path string, it *EventItem) error {
	if it.EventID == "" {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		it.EventID = "ev-" + hex.EncodeToString(b)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(it)
	if err != nil {
		return err
	}
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := fh.Write(append(b, '\n')); err != nil {
		fh.Close()
		return err
	}
	return fh.Close()
}

// Who is the local user's name, recorded as the approver.
func Who() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	for _, k := range []string{"USER", "USERNAME"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "unknown"
}
