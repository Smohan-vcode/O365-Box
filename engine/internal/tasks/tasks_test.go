package tasks

import (
	"path/filepath"
	"testing"
)

// TestBacklogIntegrity parses every task and spike and checks that each
// dependency exists and that the graph has no cycle.
func TestBacklogIntegrity(t *testing.T) {
	var files []string
	for _, dir := range []string{"tasks", "spikes"} {
		m, err := filepath.Glob(filepath.Join("..", "..", "..", dir, "[TS]*-*.md"))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	if len(files) < 50 {
		t.Fatalf("found only %d task files", len(files))
	}
	all := map[string]*Task{}
	for _, f := range files {
		tk, err := Load(f)
		if err != nil {
			t.Error(err)
			continue
		}
		switch tk.Status {
		case "TODO", "DONE", "BLOCKED":
		default:
			t.Errorf("%s: status %q", tk.ID, tk.Status)
		}
		all[tk.ID] = tk
	}
	for _, tk := range all {
		for _, d := range tk.DependsOn {
			if all[d] == nil {
				t.Errorf("%s depends on unknown %s", tk.ID, d)
			}
		}
	}
	state := map[string]int{}
	var visit func(id string, path []string)
	visit = func(id string, path []string) {
		switch state[id] {
		case 1:
			t.Errorf("dependency cycle: %v -> %s", path, id)
			return
		case 2:
			return
		}
		state[id] = 1
		if tk := all[id]; tk != nil {
			for _, d := range tk.DependsOn {
				visit(d, append(path, id))
			}
		}
		state[id] = 2
	}
	for id := range all {
		visit(id, nil)
	}
}

func TestFind(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	tk, err := Find(root, "T1-02")
	if err != nil {
		t.Fatal(err)
	}
	if len(tk.ConfigRequired) == 0 {
		t.Error("T1-02 should list config_required")
	}
	if _, err := Find(root, "T99-99"); err == nil {
		t.Error("want error for unknown task")
	}
}
