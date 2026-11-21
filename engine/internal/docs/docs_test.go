package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
	"github.com/Smohan-vcode/O365-Box/engine/internal/repo"
	"gopkg.in/yaml.v3"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := repo.FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func roleNames(t *testing.T, root string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "defaults", "roles.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Roles []struct {
			Name string `yaml:"name"`
		} `yaml:"roles"`
	}
	if err := yaml.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, x := range r.Roles {
		out = append(out, x.Name)
	}
	return out
}

// TestRepositoryTemplates checks every template in defaults/policies.
func TestRepositoryTemplates(t *testing.T) {
	root := repoRoot(t)
	cat, err := policy.Load(filepath.Join(root, "policy"), "policy")
	if err != nil {
		t.Fatal(err)
	}
	_, _, reqIDs := cat.IDs()
	docs, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) < 21 {
		t.Fatalf("index holds %d documents, want at least the 21 policies", len(docs))
	}
	known, err := Placeholders(root)
	if err != nil {
		t.Fatal(err)
	}
	var problems []string
	problems = append(problems, Validate(docs, reqIDs, roleNames(t, root))...)
	problems = append(problems, CheckPlaceholders(docs, known)...)
	problems = append(problems, CheckControls(docs, cat.Frameworks)...)
	for _, id := range Uncovered(docs, MustCover(cat.Requirements)) {
		problems = append(problems, id+": procedural requirement is covered by no document")
	}
	for _, p := range problems {
		t.Error(p)
	}
}

// TestControlsComplete checks that each document lists every soc2 and
// iso27001_2022 control the framework files map to its requirements.
func TestControlsComplete(t *testing.T) {
	root := repoRoot(t)
	cat, err := policy.Load(filepath.Join(root, "policy"), "policy")
	if err != nil {
		t.Fatal(err)
	}
	docs, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		reqs := set(d.Requirements)
		for _, fwID := range []string{"soc2", "iso27001_2022"} {
			fw := cat.Framework(fwID)
			if fw == nil {
				t.Fatalf("framework %s missing", fwID)
			}
			have := set(d.Controls[fwID])
			for _, c := range fw.Controls {
				for _, r := range c.Requirements {
					if reqs[r] && !have[c.ID] {
						t.Errorf("%s: %s control %s maps %s but is not listed", d.ID, fwID, c.ID, r)
						break
					}
				}
			}
		}
	}
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, s := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const goodMatter = `---
id: PRC-01
title: Test procedure
version: "1.0"
owner_role: control-owner
approver_role: owner
review_months: 12
requirements: [INC-01]
controls: {soc2: [CC7.3]}
enforced_by: [F17, manual]
records: ["ControlTasks:incident_exercise yearly"]
---
`

func TestValidateFindsProblems(t *testing.T) {
	index := `policies:
  - {id: PRC-01, title: Test procedure, kind: procedure, file: d/PRC-01.md, requirements: [INC-01]}
  - {id: PRC-02, title: Missing, kind: procedure, file: d/PRC-02.md, requirements: []}
  - {id: PRC-03, title: Wrong, kind: procedure, file: d/PRC-03.md, requirements: [XXX-99]}
`
	bad := strings.NewReplacer("id: PRC-01", "id: PRC-09", "owner_role: control-owner", "owner_role: wizard",
		"[INC-01]", "[XXX-99]", "[F17, manual]", "[somebody]").Replace(goodMatter)
	dir := writeTree(t, map[string]string{
		IndexFile:     index,
		"d/PRC-01.md": goodMatter + "Report to {{dictionary.role.owner.email}} at once.\n",
		"d/PRC-03.md": bad + "Write to help@example.com or visit example.com.\nAsk {{dictionary.role.wizard.email}}.\n{{oops}}\n",
	})
	docs, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(Validate(docs, []string{"INC-01"}, []string{"owner", "control-owner"}), "\n")
	for _, want := range []string{
		"PRC-02: file d/PRC-02.md cannot be read",
		`PRC-03: front matter id "PRC-09" differs`,
		`PRC-03: owner_role "wizard" is not a role`,
		"PRC-03: unknown requirement XXX-99",
		`PRC-03: enforced_by "somebody"`,
		`PRC-03: line 1: literal email address "help@example.com"`,
		`names unknown role "wizard"`,
		"placeholder {{oops}} is not",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "PRC-01:") {
		t.Errorf("good document reported:\n%s", got)
	}
	if u := Uncovered(docs, []string{"INC-01", "INC-02"}); len(u) != 1 || u[0] != "INC-02" {
		t.Errorf("Uncovered = %v, want [INC-02]", u)
	}
	unknown := CheckPlaceholders(docs[:1], map[string]string{})
	if len(unknown) != 1 {
		t.Errorf("CheckPlaceholders = %v, want one unlisted key", unknown)
	}
}

func TestDomainOutsidePlaceholder(t *testing.T) {
	d := Doc{ID: "POL-01", Body: "See box-config.yaml, e.g. the README.md.\nMail {{dictionary.company.primary_domain}}.\nVisit portal.contoso.com today.\n"}
	got := checkBody(d, map[string]bool{})
	if len(got) != 1 || !strings.Contains(got[0], "line 3") {
		t.Errorf("checkBody = %v, want one finding on line 3", got)
	}
}

func TestMissingFrontMatterKey(t *testing.T) {
	matter := strings.Replace(goodMatter, "review_months: 12\n", "", 1)
	dir := writeTree(t, map[string]string{
		IndexFile:     "policies:\n  - {id: PRC-01, title: Test procedure, kind: procedure, file: d/PRC-01.md, requirements: [INC-01]}\n",
		"d/PRC-01.md": matter,
	})
	docs, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(Validate(docs, []string{"INC-01"}, []string{"owner", "control-owner"}), "\n")
	if !strings.Contains(got, "front matter has no review_months") {
		t.Errorf("got %s", got)
	}
}
