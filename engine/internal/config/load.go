// Package config loads and validates the configuration document
// box-config.yaml (SPEC §9.1).
package config

import (
	"bytes"
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Document is a loaded configuration document.
type Document struct {
	File   string         // path it was read from, for messages
	Raw    map[string]any // generic form, used for the schema and path lookups
	Config Config         // typed form, used for semantic checks
	lines  map[string]int // configuration path -> line number
}

// Load reads and parses a configuration document.
func Load(path string) (*Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(path, b)
}

// Parse parses a configuration document from bytes.
func Parse(name string, b []byte) (*Document, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(b, &node); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if len(node.Content) == 0 {
		return nil, fmt.Errorf("%s: empty document", name)
	}
	var raw map[string]any
	if err := node.Decode(&raw); err != nil {
		return nil, fmt.Errorf("%s: the document must be a mapping: %w", name, err)
	}
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&cfg); err != nil {
		// Type mismatches are reported by the schema; keep what decoded.
		_ = err
	}
	d := &Document{File: name, Raw: raw, Config: cfg, lines: map[string]int{}}
	indexLines(node.Content[0], "", d.lines)
	return d, nil
}

// Line returns the line of the nearest known ancestor of path, or 0.
func (d *Document) Line(path string) int {
	for p := path; ; {
		if l, ok := d.lines[p]; ok {
			return l
		}
		if p == "" {
			return 0
		}
		p = parentPath(p)
	}
}

func indexLines(n *yaml.Node, path string, out map[string]int) {
	out[path] = n.Line
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			p := joinKey(path, k.Value)
			out[p] = k.Line
			indexLines(v, p, out)
			out[p] = k.Line
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			indexLines(c, path+"["+strconv.Itoa(i)+"]", out)
		}
	}
}

func joinKey(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func parentPath(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		switch p[i] {
		case '.':
			return p[:i]
		case '[':
			return p[:i]
		}
	}
	return ""
}
