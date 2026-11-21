package jurisdictions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const repoRoot = "../../.."

func TestRepositoryRulesFiles(t *testing.T) {
	issues, err := Check(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range issues {
		t.Error(i)
	}
}

func TestFirstReleaseHierarchy(t *testing.T) {
	for _, c := range FirstRelease {
		r, err := Load(repoRoot, c)
		if err != nil {
			t.Fatal(err)
		}
		want := ""
		switch {
		case strings.HasPrefix(c, "us-"):
			want = "us"
		case strings.HasPrefix(c, "ca-"):
			want = "ca"
		case c == "de" || c == "fr" || c == "ie" || c == "nl" || c == "es" || c == "it":
			want = "eu"
		}
		if r.Extends != want {
			t.Errorf("%s: extends %q, want %q", c, r.Extends, want)
		}
	}
}

func write(t *testing.T, root, code, body string) {
	t.Helper()
	dir := filepath.Join(root, "defaults", "jurisdictions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, code+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckFindsProblems(t *testing.T) {
	root := t.TempDir()
	write(t, root, "xx", `code: yy
name: X
extends: zz
status: FINAL
sources: [a]
correspondence: {required: [colour]}
privacy: {regime: r, regulator: g}
incident_notifications:
  - {regime: r, what: w, to: "a | b", deadline: ""}
`)
	write(t, root, "bad", "code: bad\nnmae: typo\n")
	issues, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(issues, "\n")
	for _, want := range []string{
		"us: first-release jurisdiction has no rules file",
		`xx: code "yy" does not match`,
		`xx: extends "zz"`,
		`xx: status "FINAL"`,
		`xx: unknown correspondence field "colour"`,
		"xx: incident_notifications[0] has no deadline",
		"xx: incident_notifications[0] has no applies_when",
		"xx: incident_notifications[0].to must be one line",
		"bad: ",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing issue %q in:\n%s", want, joined)
		}
	}
}
