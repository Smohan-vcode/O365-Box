package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Value is one value found at a concrete path.
type Value struct {
	Path  string
	Value any
}

// Resolve finds every value matching a path such as users[*].primary or
// tenant.tenant_id. "[*]" matches each element; "[n]" one element. Missing
// segments yield a Value with nil, so callers can report them as empty.
func Resolve(raw map[string]any, path string) ([]Value, error) {
	segs, err := splitPath(path)
	if err != nil {
		return nil, err
	}
	cur := []Value{{Path: "", Value: map[string]any(raw)}}
	for _, s := range segs {
		var next []Value
		for _, c := range cur {
			switch {
			case s == "[*]":
				arr, _ := c.Value.([]any)
				if len(arr) == 0 {
					next = append(next, Value{Path: c.Path + "[*]", Value: nil})
				}
				for i, e := range arr {
					next = append(next, Value{Path: c.Path + "[" + strconv.Itoa(i) + "]", Value: e})
				}
			case strings.HasPrefix(s, "["):
				n, _ := strconv.Atoi(strings.Trim(s, "[]"))
				arr, _ := c.Value.([]any)
				var e any
				if n < len(arr) {
					e = arr[n]
				}
				next = append(next, Value{Path: c.Path + s, Value: e})
			default:
				m, _ := c.Value.(map[string]any)
				var e any
				if m != nil {
					e = m[s]
				}
				next = append(next, Value{Path: joinKey(c.Path, s), Value: e})
			}
		}
		cur = next
	}
	return cur, nil
}

func splitPath(path string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			return nil, fmt.Errorf("invalid path %q", path)
		}
		name := part
		var idx []string
		if i := strings.Index(part, "["); i >= 0 {
			name, idx = part[:i], nil
			rest := part[i:]
			for rest != "" {
				j := strings.Index(rest, "]")
				if !strings.HasPrefix(rest, "[") || j < 0 {
					return nil, fmt.Errorf("invalid path %q", path)
				}
				in := rest[1:j]
				if in != "*" {
					if _, err := strconv.Atoi(in); err != nil {
						return nil, fmt.Errorf("invalid index in path %q", path)
					}
				}
				idx = append(idx, rest[:j+1])
				rest = rest[j+1:]
			}
		}
		if name != "" {
			out = append(out, name)
		}
		out = append(out, idx...)
	}
	return out, nil
}

// IsEmpty reports whether a value counts as not filled in.
func IsEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}
