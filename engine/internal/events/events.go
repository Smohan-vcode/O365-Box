// Package events defines the orchestrator's event stream: one JSON object
// per line, read by the TUI, the CLI's --json output, CI logs and the
// Setup list (SPEC §14.2).
package events

import (
	"encoding/json"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kinds of event.
const (
	RunStarted  = "run.started"
	StepPlanned = "step.planned"
	StepStarted = "step.started"
	StepProg    = "step.progress"
	StepChanged = "step.changed"
	GateWaiting = "gate.waiting"
	GatePassed  = "gate.passed"
	StepDone    = "step.done"
	StepFailed  = "step.failed"
	StepSkipped = "step.skipped"
	RunFinished = "run.finished"
	Log         = "log"
)

// Event is one line of the stream.
type Event struct {
	T     time.Time      `json:"t"`
	Run   string         `json:"run"`
	Step  string         `json:"step,omitempty"`
	Gate  string         `json:"gate,omitempty"`
	Ev    string         `json:"ev"`
	Msg   string         `json:"msg,omitempty"`
	Level string         `json:"level,omitempty"`
	Done  int            `json:"done,omitempty"`
	Of    int            `json:"of,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

// Sink receives events.
type Sink interface{ Emit(Event) }

// SinkFunc adapts a function.
type SinkFunc func(Event)

func (f SinkFunc) Emit(e Event) { f(e) }

// Multi sends each event to every sink.
type Multi []Sink

func (m Multi) Emit(e Event) {
	for _, s := range m {
		s.Emit(e)
	}
}

// JSONLines writes events as JSON lines. Safe for concurrent use.
type JSONLines struct {
	mu sync.Mutex
	w  io.Writer
}

// NewJSONLines returns a sink writing to w.
func NewJSONLines(w io.Writer) *JSONLines { return &JSONLines{w: w} }

func (j *JSONLines) Emit(e Event) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	_, _ = j.w.Write(append(b, '\n'))
}

// Recorder keeps events in memory, for tests and summaries.
type Recorder struct {
	mu     sync.Mutex
	Events []Event
}

func (r *Recorder) Emit(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Events = append(r.Events, e)
}

// Of returns the recorded events of one kind.
func (r *Recorder) Of(kind string) []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Event
	for _, e := range r.Events {
		if e.Ev == kind {
			out = append(out, e)
		}
	}
	return out
}

// Redactor removes secrets and never_store fields before events reach any
// sink (SPEC §14.2, §21). Register every secret value as it becomes known.
type Redactor struct {
	emitMu    sync.Mutex // serialises delivery, so sinks need no locking
	mu        sync.RWMutex
	next      Sink
	secrets   []string
	neverKeys map[string]bool
}

// NewRedactor wraps next. neverStore lists field names whose values are
// dropped wherever they appear in event data.
func NewRedactor(next Sink, neverStore []string) *Redactor {
	r := &Redactor{next: next, neverKeys: map[string]bool{}}
	for _, k := range neverStore {
		r.neverKeys[strings.ToLower(k)] = true
	}
	return r
}

// AddSecret registers a value that must never appear in an event.
func (r *Redactor) AddSecret(v string) {
	if len(v) < 6 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.secrets = append(r.secrets, v)
	sort.Slice(r.secrets, func(i, j int) bool { return len(r.secrets[i]) > len(r.secrets[j]) })
}

// Text redacts registered secrets from s.
func (r *Redactor) Text(s string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, sec := range r.secrets {
		s = strings.ReplaceAll(s, sec, "[redacted]")
	}
	return s
}

func (r *Redactor) Emit(e Event) {
	e.Msg = r.Text(e.Msg)
	if e.Data != nil {
		e.Data, _ = r.value(e.Data).(map[string]any)
	}
	r.emitMu.Lock()
	defer r.emitMu.Unlock()
	r.next.Emit(e)
}

func (r *Redactor) value(v any) any {
	switch x := v.(type) {
	case string:
		return r.Text(x)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if r.neverKeys[strings.ToLower(k)] {
				out[k] = "[not stored]"
				continue
			}
			out[k] = r.value(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = r.value(val)
		}
		return out
	}
	return v
}
