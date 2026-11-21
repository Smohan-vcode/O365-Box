// Package state keeps each step's last result for one tenant: the local
// cache of the Setup list (SPEC §14.3). It holds no secrets.
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Step statuses.
const (
	Done    = "DONE"
	Failed  = "FAILED"
	Skipped = "SKIPPED"
	Waiting = "WAITING" // a gate has not passed
	Drifted = "DRIFTED"
	Planned = "PLANNED" // plan ran, apply has not
	Blocked = "BLOCKED" // a dependency did not finish
)

// Record is one step's last result.
type Record struct {
	Step     string    `json:"step"`
	Status   string    `json:"status"`
	PlanHash string    `json:"plan_hash,omitempty"`
	Changes  int       `json:"changes"`
	Evidence []string  `json:"evidence,omitempty"`
	Reason   string    `json:"reason,omitempty"`
	Run      string    `json:"run"`
	At       time.Time `json:"at"`
}

// File is a state store backed by one JSON file.
type File struct {
	mu   sync.Mutex
	path string
	data struct {
		Version int                `json:"version"`
		Steps   map[string]*Record `json:"steps"`
	}
}

// Open loads path, or starts empty if it does not exist.
func Open(path string) (*File, error) {
	f := &File{path: path}
	f.data.Version = 1
	f.data.Steps = map[string]*Record{}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &f.data); err != nil {
		return nil, err
	}
	if f.data.Steps == nil {
		f.data.Steps = map[string]*Record{}
	}
	return f, nil
}

// Get returns a copy of a step's record, or nil.
func (f *File) Get(step string) *Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.data.Steps[step]
	if r == nil {
		return nil
	}
	c := *r
	return &c
}

// All returns every record, sorted by step.
func (f *File) All() []Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Record, 0, len(f.data.Steps))
	for _, r := range f.data.Steps {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Step < out[j].Step })
	return out
}

// Put stores a record and writes the file atomically.
func (f *File) Put(r Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data.Steps[r.Step] = &r
	return f.write()
}

func (f *File) write() error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

// GateRecord returns the confirmation record of a gate without a check.
func (f *File) GateRecord(id string) *Record {
	if f == nil {
		return nil
	}
	return f.Get("gate:" + id)
}

// ConfirmGate records that a person confirmed a gate.
func (f *File) ConfirmGate(id, run string, at time.Time) error {
	return f.Put(Record{Step: "gate:" + id, Status: Done, Reason: "confirmed by a person", Run: run, At: at})
}

// DefaultPath is the local cache for a tenant under the user's cache dir.
func DefaultPath(tenant string) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	if tenant == "" {
		tenant = "unknown-tenant"
	}
	return filepath.Join(dir, "box", tenant, "state.json"), nil
}
