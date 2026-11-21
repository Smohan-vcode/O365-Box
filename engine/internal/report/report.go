// Package report holds the issue type shared by every check and its
// human and JSON renderings.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// Issue is one finding. Path is a configuration path (a.b[0].c) when the
// finding is about the configuration; File and Line locate it on disk.
type Issue struct {
	Check   string `json:"check"`
	Path    string `json:"path,omitempty"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

func (i Issue) String() string {
	loc := ""
	switch {
	case i.File != "" && i.Line > 0:
		loc = fmt.Sprintf("%s:%d", i.File, i.Line)
	case i.File != "":
		loc = i.File
	}
	if i.Path != "" {
		if loc != "" {
			loc += " "
		}
		loc += i.Path
	}
	if loc == "" {
		return i.Message
	}
	return loc + ": " + i.Message
}

// Sort orders issues by file, line, path and message for stable output.
func Sort(issues []Issue) {
	sort.SliceStable(issues, func(a, b int) bool {
		x, y := issues[a], issues[b]
		if x.File != y.File {
			return x.File < y.File
		}
		if x.Line != y.Line {
			return x.Line < y.Line
		}
		if x.Path != y.Path {
			return x.Path < y.Path
		}
		return x.Message < y.Message
	})
}

// Result is what a command prints.
type Result struct {
	Command string  `json:"command"`
	OK      bool    `json:"ok"`
	Summary string  `json:"summary"`
	Issues  []Issue `json:"issues"`
}

// Write renders the result as text or JSON.
func (r Result) Write(w io.Writer, asJSON bool) error {
	if r.Issues == nil {
		r.Issues = []Issue{}
	}
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	for _, i := range r.Issues {
		if _, err := fmt.Fprintln(w, "  ✗ "+i.String()); err != nil {
			return err
		}
	}
	mark := "✓"
	if !r.OK {
		mark = "✗"
	}
	_, err := fmt.Fprintf(w, "%s %s: %s\n", mark, r.Command, r.Summary)
	return err
}
