// Package providers supervises provider workers: one pwsh process per
// provider, speaking JSON lines on stdin and stdout (SPEC §3, spike S-05).
package providers

import (
	"encoding/json"
	"fmt"
)

// Marker starts every protocol line a worker writes. Lines without it are
// stray console output from a module.
const Marker = "\x1eBOX:"

// Request is one line sent to a worker.
type Request struct {
	ID   string `json:"id"`
	Op   string `json:"op"` // hello | auth | invoke | shutdown
	Data any    `json:"data,omitempty"`
}

// Invoke is the data of an "invoke" request.
type Invoke struct {
	Step      string `json:"step"`
	Operation string `json:"operation"` // test | plan | apply | verify | evidence
	Input     any    `json:"input,omitempty"`
}

// Event is one line received from a worker.
type Event struct {
	ID    string          `json:"id"`
	Ev    string          `json:"ev"` // ready | progress | log | result | error
	Msg   string          `json:"msg,omitempty"`
	Level string          `json:"level,omitempty"`
	Done  int             `json:"done,omitempty"`
	Of    int             `json:"of,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *WorkerError    `json:"error,omitempty"`
}

// WorkerError is an error reported by the worker or the provider.
type WorkerError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (e *WorkerError) Error() string { return fmt.Sprintf("%s: %s", e.Type, e.Message) }

// Hello is the result of a "hello" request.
type Hello struct {
	PID      int    `json:"pid"`
	Pwsh     string `json:"pwsh"`
	Provider string `json:"provider"`
	Modules  []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"modules"`
}
