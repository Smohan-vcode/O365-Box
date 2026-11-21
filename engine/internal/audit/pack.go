package audit

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
)

// Pack is everything the pack writer needs.
type Pack struct {
	Tenant       string
	Organisation string
	Subject      string // who signed in
	ReadBy       string // "box audit <version>"
	Period       Period
	ReadAt       time.Time
	Framework    *policy.Framework
	Assessment   *Assessment
	Inventories  []*Inventory
	Settings     map[string]*SettingRead
	Steps        map[string]StepOutcome
	Notes        []string // what the reader should know (not read, warnings)
}

// ManifestFile lists every other file of the pack with its SHA-256.
const ManifestFile = "manifest.sha256"

// CheckOutDir returns out as an absolute path, refusing a folder inside the
// repository (R1: the pack describes one company) and a folder that holds
// files already.
func CheckOutDir(repoRoot, out string) (string, error) {
	if strings.TrimSpace(out) == "" {
		return "", errors.New("--out is required: the folder the pack is written to")
	}
	abs, err := filepath.Abs(out)
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	// Resolve symbolic links on the part of the path that exists.
	real, rest := abs, ""
	for {
		if r, err := filepath.EvalSymlinks(real); err == nil {
			real = filepath.Join(r, rest)
			break
		}
		parent := filepath.Dir(real)
		if parent == real {
			real = abs
			break
		}
		rest = filepath.Join(filepath.Base(real), rest)
		real = parent
	}
	for _, p := range []string{abs, real} {
		rel, err := filepath.Rel(root, p)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("--out %s is inside the repository; the pack describes a company and is written elsewhere", out)
		}
	}
	if entries, err := os.ReadDir(abs); err == nil && len(entries) > 0 {
		return "", fmt.Errorf("--out %s is not empty; choose a new or empty folder", out)
	}
	return abs, nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeName(s string) string {
	s = unsafeName.ReplaceAllString(s, "_")
	if s == "" || s == "." || s == ".." {
		return "_"
	}
	return s
}

type writer struct {
	dir   string
	files []string
}

func (w *writer) write(rel string, b []byte) error {
	p := filepath.Join(w.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		return err
	}
	w.files = append(w.files, rel)
	return nil
}

// evidenceFile is an inventory as written: stamped with the tenant, the
// time and the queries (the inventory carries read_at and queries).
type evidenceFile struct {
	TenantID string `json:"tenant_id"`
	ReadBy   string `json:"read_by"`
	Period   string `json:"period"`
	*Inventory
}

type settingsFile struct {
	TenantID string         `json:"tenant_id"`
	ReadBy   string         `json:"read_by"`
	ReadAt   time.Time      `json:"read_at"`
	Queries  []string       `json:"queries"`
	Control  string         `json:"control"`
	Settings []*SettingRead `json:"settings"`
}

type controlFile struct {
	TenantID string         `json:"tenant_id"`
	ReadBy   string         `json:"read_by"`
	ReadAt   time.Time      `json:"read_at"`
	Period   string         `json:"period"`
	Queries  []string       `json:"queries"`
	Control  *ControlStatus `json:"control"`
}

type stepFile struct {
	TenantID string    `json:"tenant_id"`
	ReadBy   string    `json:"read_by"`
	ReadAt   time.Time `json:"read_at"`
	Queries  []string  `json:"queries"`
	StepOutcome
}

func jsonBytes(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err := enc.Encode(v)
	return buf.Bytes(), err
}

func (p *Pack) csvBytes(inv *Inventory) ([]byte, error) {
	cols := map[string]bool{}
	for _, r := range inv.Rows {
		for k := range r {
			cols[k] = true
		}
	}
	header := make([]string, 0, len(cols))
	for k := range cols {
		header = append(header, k)
	}
	sort.Strings(header)
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "# tenant_id: %s\n# read_at: %s\n# read_by: %s\n", p.Tenant, inv.ReadAt.Format(time.RFC3339), p.ReadBy)
	for _, q := range inv.Queries {
		fmt.Fprintf(&buf, "# query: %s\n", q)
	}
	w := csv.NewWriter(&buf)
	_ = w.Write(header)
	for _, r := range inv.Rows {
		rec := make([]string, len(header))
		for i, k := range header {
			rec[i] = cell(r[k])
		}
		_ = w.Write(rec)
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func cell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool, float64, int:
		return fmt.Sprint(x)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// inventoryFiles writes one inventory under dir and returns the names.
func (p *Pack) inventoryFiles(w *writer, dir string, inv *Inventory) ([]string, error) {
	b, err := jsonBytes(evidenceFile{TenantID: p.Tenant, ReadBy: p.ReadBy, Period: p.Period.String(), Inventory: inv})
	if err != nil {
		return nil, err
	}
	name := dir + "/" + safeName(inv.ID) + ".json"
	if err := w.write(name, b); err != nil {
		return nil, err
	}
	names := []string{name}
	if len(inv.Rows) > 0 {
		c, err := p.csvBytes(inv)
		if err != nil {
			return nil, err
		}
		cn := dir + "/" + safeName(inv.ID) + ".csv"
		if err := w.write(cn, c); err != nil {
			return nil, err
		}
		names = append(names, cn)
	}
	return names, nil
}

// Write writes the pack into dir (which CheckOutDir accepted) and returns
// the files written, relative to dir. Nothing is written outside dir.
func Write(dir string, p *Pack) ([]string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	w := &writer{dir: dir}
	invByID := map[string]*Inventory{}
	for _, inv := range p.Inventories {
		invByID[inv.ID] = inv
	}
	used := map[string]bool{}
	for _, c := range p.Assessment.Controls {
		cdir := "evidence/" + safeName(c.ID)
		var settings []*SettingRead
		var queries []string
		seenSetting, seenInv := map[string]bool{}, map[string]bool{}
		for _, r := range c.Requirements {
			for _, s := range r.Settings {
				if !seenSetting[s.ID] {
					seenSetting[s.ID] = true
					settings = append(settings, s)
					q := s.Query
					if q == "" {
						q = "no automated read (" + s.Detail + ")"
					}
					queries = append(queries, s.ID+": "+q)
				}
			}
			for _, id := range r.Inventories {
				if seenInv[id] || invByID[id] == nil {
					continue
				}
				seenInv[id] = true
				used[id] = true
				names, err := p.inventoryFiles(w, cdir, invByID[id])
				if err != nil {
					return nil, err
				}
				c.Evidence = append(c.Evidence, names...)
			}
		}
		if len(settings) > 0 {
			b, err := jsonBytes(settingsFile{TenantID: p.Tenant, ReadBy: p.ReadBy, ReadAt: p.ReadAt, Queries: queries, Control: c.ID, Settings: settings})
			if err != nil {
				return nil, err
			}
			name := cdir + "/settings.json"
			if err := w.write(name, b); err != nil {
				return nil, err
			}
			c.Evidence = append(c.Evidence, name)
		}
		name := cdir + "/control.json"
		c.Evidence = append(c.Evidence, name)
		b, err := jsonBytes(controlFile{TenantID: p.Tenant, ReadBy: p.ReadBy, ReadAt: p.ReadAt, Period: p.Period.String(),
			Queries: []string{"requirements of " + p.Framework.ID + " " + c.ID + " in policy/requirements.yaml; settings and inventories above"}, Control: c})
		if err != nil {
			return nil, err
		}
		if err := w.write(name, b); err != nil {
			return nil, err
		}
	}
	// Inventories no control names, and each step's raw verify result.
	for _, inv := range p.Inventories {
		if !used[inv.ID] {
			if _, err := p.inventoryFiles(w, "evidence/tenant", inv); err != nil {
				return nil, err
			}
		}
	}
	stepIDs := make([]string, 0, len(p.Steps))
	for id := range p.Steps {
		stepIDs = append(stepIDs, id)
	}
	sort.Strings(stepIDs)
	for _, id := range stepIDs {
		o := p.Steps[id]
		b, err := jsonBytes(stepFile{TenantID: p.Tenant, ReadBy: p.ReadBy, ReadAt: p.ReadAt,
			Queries: []string{id + " verify (the same check the build uses), in audit mode"}, StepOutcome: o})
		if err != nil {
			return nil, err
		}
		if err := w.write("evidence/settings/"+safeName(id)+".json", b); err != nil {
			return nil, err
		}
	}
	all := make([]*SettingRead, 0, len(p.Settings))
	for _, s := range p.Settings {
		all = append(all, s)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	b, err := jsonBytes(settingsFile{TenantID: p.Tenant, ReadBy: p.ReadBy, ReadAt: p.ReadAt,
		Queries: []string{"every catalogue setting (policy/settings), read through its mapping (expected-state/setting-ids)"}, Control: "all", Settings: all})
	if err != nil {
		return nil, err
	}
	if err := w.write("evidence/settings/all-settings.json", b); err != nil {
		return nil, err
	}
	if b, err = p.controlsCSV(); err != nil {
		return nil, err
	}
	if err := w.write("controls.csv", b); err != nil {
		return nil, err
	}
	if err := w.write("gaps.md", []byte(p.gapsMarkdown())); err != nil {
		return nil, err
	}
	html, err := p.indexHTML()
	if err != nil {
		return nil, err
	}
	if err := w.write("index.html", html); err != nil {
		return nil, err
	}
	if err := writeManifest(dir, w.files); err != nil {
		return nil, err
	}
	return append(w.files, ManifestFile), nil
}

func (p *Pack) controlsCSV() ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"framework", "control", "title", "status", "requirements", "documents_needed", "evidence_files", "gap"})
	for _, c := range p.Assessment.Controls {
		var reqs []string
		for _, r := range c.Requirements {
			reqs = append(reqs, r.ID+"="+r.Status)
		}
		_ = w.Write([]string{c.Framework, c.ID, c.Title, c.Status, strings.Join(reqs, "; "), strings.Join(c.Documents, "; "),
			strings.Join(c.Evidence, "; "), c.Gap})
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func (p *Pack) gapsMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Gaps: %s\n\n", p.Framework.Title)
	fmt.Fprintf(&b, "Tenant %s, period %s, read %s by %s (read only).\n\n", p.Tenant, p.Period, p.ReadAt.Format(time.RFC3339), p.ReadBy)
	b.WriteString("Ordered by risk: not met before partly met, not read and document needed; core requirements before enhanced; high findings first. ")
	b.WriteString("Each gap ends with the proposal Company in a Box would make to fix it. Nothing has been changed in the tenant.\n\n")
	gaps := p.Assessment.Gaps()
	if len(gaps) == 0 {
		b.WriteString("No gaps: every requirement this framework maps is met.\n")
	}
	for i, g := range gaps {
		r := g.Requirement
		fmt.Fprintf(&b, "## %d. %s: %s (%s, risk %d)\n\n", i+1, r.ID, r.Statement, r.Status, r.Risk)
		fmt.Fprintf(&b, "- Controls: %s\n", strings.Join(g.Controls, ", "))
		fmt.Fprintf(&b, "- Domain: %s; tier %s; type %s\n", r.Domain, r.Tier, r.Type)
		if r.Gap != "" {
			fmt.Fprintf(&b, "- Gap: %s\n", r.Gap)
		}
		if r.Fix != "" {
			fmt.Fprintf(&b, "- %s\n", r.Fix)
		}
		if len(g.Controls) > 0 {
			fmt.Fprintf(&b, "- Evidence: evidence/%s/\n", safeName(g.Controls[0]))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func writeManifest(dir string, files []string) error {
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	var buf bytes.Buffer
	for _, rel := range sorted {
		sum, err := fileSum(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		fmt.Fprintf(&buf, "%s  %s\n", sum, rel)
	}
	return os.WriteFile(filepath.Join(dir, ManifestFile), buf.Bytes(), 0o600)
}

func fileSum(p string) (string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// VerifyManifest checks every file of a pack against manifest.sha256 and
// reports a changed, missing or unlisted file. The same check runs with
// `sha256sum -c manifest.sha256` inside the folder.
func VerifyManifest(dir string) ([]string, error) {
	f, err := os.Open(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	listed := map[string]bool{}
	var problems []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		sum, rel, ok := strings.Cut(sc.Text(), "  ")
		if !ok {
			problems = append(problems, "malformed line: "+sc.Text())
			continue
		}
		listed[rel] = true
		got, err := fileSum(filepath.Join(dir, filepath.FromSlash(rel)))
		switch {
		case err != nil:
			problems = append(problems, rel+": missing")
		case got != sum:
			problems = append(problems, rel+": changed")
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if rel != ManifestFile && !listed[rel] {
			problems = append(problems, rel+": not in the manifest")
		}
		return nil
	})
	sort.Strings(problems)
	return problems, err
}
