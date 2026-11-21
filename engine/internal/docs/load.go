// Package docs loads the policy and procedure templates indexed in
// defaults/policies.yaml (SPEC §15, §23) and checks them. Each template is
// a Markdown file with YAML front matter; its body holds placeholders that
// the renderer fills with company values at render time.
package docs

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Index and placeholder files, relative to the repository root.
const (
	IndexFile        = "defaults/policies.yaml"
	PlaceholdersFile = "defaults/policies/PLACEHOLDERS.md"
)

// Kinds of document.
const (
	KindPolicy    = "policy"
	KindProcedure = "procedure"
)

// Doc is one policy or procedure template. ID, Title, Kind and File come
// from the index entry; the other fields from the file's front matter;
// Body is the Markdown after the front matter.
type Doc struct {
	ID           string
	Title        string
	Version      string
	OwnerRole    string
	ApproverRole string
	ReviewMonths int
	Requirements []string
	Controls     map[string][]string
	EnforcedBy   []string
	Records      []string
	Kind         string
	File         string
	Body         string

	// From the file, kept for Validate.
	fmID       string
	fmTitle    string
	indexReqs  []string
	problems   []string
	hasMatter  bool
	missingKey []string
}

type indexEntry struct {
	ID           string   `yaml:"id"`
	Title        string   `yaml:"title"`
	Kind         string   `yaml:"kind"`
	File         string   `yaml:"file"`
	Requirements []string `yaml:"requirements"`
	Summary      string   `yaml:"summary"`
}

type frontMatter struct {
	ID           string              `yaml:"id"`
	Title        string              `yaml:"title"`
	Version      string              `yaml:"version"`
	OwnerRole    string              `yaml:"owner_role"`
	ApproverRole string              `yaml:"approver_role"`
	ReviewMonths int                 `yaml:"review_months"`
	Requirements []string            `yaml:"requirements"`
	Controls     map[string][]string `yaml:"controls"`
	EnforcedBy   []string            `yaml:"enforced_by"`
	Records      []string            `yaml:"records"`
}

// FrontMatterKeys are the keys every template's front matter carries, and
// the only ones it may carry.
var FrontMatterKeys = []string{"id", "title", "version", "owner_role", "approver_role",
	"review_months", "requirements", "controls", "enforced_by", "records"}

// Load reads the index under the repository root and every file it names.
// It returns an error only when the index cannot be read; problems with a
// template (a missing file, bad front matter) are reported by Validate.
func Load(root string) ([]Doc, error) {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(IndexFile)))
	if err != nil {
		return nil, err
	}
	var idx struct {
		Policies []indexEntry `yaml:"policies"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&idx); err != nil {
		return nil, fmt.Errorf("%s: %w", IndexFile, err)
	}
	docs := make([]Doc, 0, len(idx.Policies))
	for _, e := range idx.Policies {
		d := Doc{ID: e.ID, Title: e.Title, Kind: e.Kind, File: e.File, indexReqs: e.Requirements}
		if e.File == "" {
			d.problems = append(d.problems, "no file in the index")
		} else if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(e.File))); err != nil {
			d.problems = append(d.problems, "file "+e.File+" cannot be read: "+err.Error())
		} else {
			parse(&d, b)
		}
		docs = append(docs, d)
	}
	return docs, nil
}

// parse splits b into front matter and body and fills d.
func parse(d *Doc, b []byte) {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		d.problems = append(d.problems, "no front matter (the file must start with a --- line)")
		d.Body = s
		return
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		d.problems = append(d.problems, "front matter is not closed by a --- line")
		return
	}
	matter, body := rest[:end+1], rest[end+len("\n---\n"):]
	d.Body = body

	var node yaml.Node
	if err := yaml.Unmarshal([]byte(matter), &node); err != nil {
		d.problems = append(d.problems, "front matter: "+err.Error())
		return
	}
	present := map[string]bool{}
	if len(node.Content) == 1 && node.Content[0].Kind == yaml.MappingNode {
		m := node.Content[0]
		for i := 0; i+1 < len(m.Content); i += 2 {
			present[m.Content[i].Value] = true
		}
	}
	for _, k := range FrontMatterKeys {
		if !present[k] {
			d.missingKey = append(d.missingKey, k)
		}
	}
	var fm frontMatter
	dec := yaml.NewDecoder(strings.NewReader(matter))
	dec.KnownFields(true)
	if err := dec.Decode(&fm); err != nil {
		d.problems = append(d.problems, "front matter: "+err.Error())
		return
	}
	d.hasMatter = true
	d.fmID, d.fmTitle = fm.ID, fm.Title
	d.Version = fm.Version
	d.OwnerRole, d.ApproverRole = fm.OwnerRole, fm.ApproverRole
	d.ReviewMonths = fm.ReviewMonths
	d.Requirements = fm.Requirements
	d.Controls = fm.Controls
	d.EnforcedBy = fm.EnforcedBy
	d.Records = fm.Records
}

// Placeholders reads the placeholder keys documented in
// defaults/policies/PLACEHOLDERS.md: every table row whose first cell is a
// key in backticks. It returns key -> description.
func Placeholders(root string) (map[string]string, error) {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(PlaceholdersFile)))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			continue
		}
		key := strings.TrimSpace(cells[1])
		if len(key) < 3 || key[0] != '`' || key[len(key)-1] != '`' {
			continue
		}
		out[strings.Trim(key, "`")] = strings.TrimSpace(cells[2])
	}
	return out, nil
}
