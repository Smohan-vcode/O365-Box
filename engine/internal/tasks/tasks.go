// Package tasks reads the front matter of the engine backlog in tasks/
// and spikes/.
package tasks

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Task is a task or spike's front matter.
type Task struct {
	ID             string   `yaml:"id"`
	Title          string   `yaml:"title"`
	Track          string   `yaml:"track"`
	Priority       string   `yaml:"priority"`
	Size           string   `yaml:"size"`
	Status         string   `yaml:"status"`
	DependsOn      []string `yaml:"depends_on"`
	ConfigRequired []string `yaml:"config_required"`
	Rules          []string `yaml:"rules"`
	File           string   `yaml:"-"`
}

// Find loads the task or spike with this id from root/tasks or root/spikes.
func Find(root, id string) (*Task, error) {
	for _, dir := range []string{"tasks", "spikes"} {
		p := filepath.Join(root, dir, id+".md")
		if _, err := os.Stat(p); err == nil {
			return Load(p)
		}
	}
	return nil, fmt.Errorf("no task or spike %q in tasks/ or spikes/", id)
}

// Load parses one task file's front matter.
func Load(path string) (*Task, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fm, err := frontMatter(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var t Task
	if err := yaml.Unmarshal(fm, &t); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if t.ID == "" {
		return nil, fmt.Errorf("%s: front matter has no id", path)
	}
	t.File = path
	return &t, nil
}

func frontMatter(b []byte) ([]byte, error) {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(b, []byte("---\n")) {
		return nil, errors.New("missing front matter")
	}
	rest := b[4:]
	end := bytes.Index(rest, []byte("\n---"))
	if end < 0 {
		return nil, errors.New("unterminated front matter")
	}
	return rest[:end+1], nil
}
