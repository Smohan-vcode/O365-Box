package checks

import (
	"os"
	"strings"
	"testing"

	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
	"github.com/Smohan-vcode/O365-Box/engine/internal/report"
)

func fixtureLiterals(t *testing.T) []Literal {
	t.Helper()
	b, err := os.ReadFile("testdata/literals/company.yaml")
	if err != nil {
		t.Fatal(err)
	}
	d, err := config.Parse("company.yaml", b)
	if err != nil {
		t.Fatal(err)
	}
	return LiteralsFrom(d, map[string]string{
		"company.address.street":  "12 Quarry Row",
		"company.address.country": "Nowhere",
	})
}

func has(issues []report.Issue, file string, line int, substr string) bool {
	for _, i := range issues {
		if i.File == file && i.Line == line && strings.Contains(i.Message, substr) {
			return true
		}
	}
	return false
}

func TestLiteralsFrom(t *testing.T) {
	got := map[string]bool{}
	for _, l := range fixtureLiterals(t) {
		got[l.Value] = true
	}
	for _, want := range []string{"Globex Unicorns Plc", "Globex", "globexunicorns.example", "globexunicorns",
		"11111111-2222-4333-8444-555555555555", "Pat Gallowglass", "Gallowglass", "Kim Kite", "12 Quarry Row"} {
		if !got[want] {
			t.Errorf("missing literal %q", want)
		}
	}
	for _, not := range []string{"Nowhere", "Kim", "Kite"} {
		if got[not] {
			t.Errorf("unexpected literal %q", not)
		}
	}
}

func TestLiteralsFindsPlantedValues(t *testing.T) {
	issues, n, err := Literals("testdata/literals/repo", fixtureLiterals(t), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("scanned %d files, want 3", n)
	}
	for _, w := range []struct {
		file string
		line int
		msg  string
	}{
		{"leak.md", 1, "company.legal_name"},
		{"leak.md", 2, "company.primary_domain"},
		{"leak.md", 3, "users[1] name"},
		{"leak.md", 3, "users[0] name"},
		{"secret.txt", 2, "private key"},
		{"secret.txt", 3, "assigned secret"},
	} {
		if !has(issues, w.file, w.line, w.msg) {
			t.Errorf("want %s:%d %s", w.file, w.line, w.msg)
		}
	}
	for _, i := range issues {
		if i.File == "clean.md" {
			t.Errorf("clean file flagged: %v", i)
		}
	}
}

func TestLiteralsSkip(t *testing.T) {
	issues, _, err := Literals("testdata/literals/repo", fixtureLiterals(t), ScanOptions{
		Skip: func(rel string) bool { return rel != "clean.md" && rel != "." },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Errorf("skipped files reported: %v", issues)
	}
}

func TestConnectorsFixture(t *testing.T) {
	issues, n, err := Connectors("testdata/connectors", ConnectorPaths{
		AllowList: "allow.yaml", Catalogue: "catalogue.yaml", Solution: "solution",
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("checked %d connector uses, want 5", n)
	}
	if !has(issues, "catalogue.yaml", 3, `flow X02 (Calls out) uses connector "http"`) {
		t.Errorf("catalogue issue missing: %v", issues)
	}
	if !has(issues, "solution/flow.json", 5, `connector API "shared_sendgrid"`) {
		t.Errorf("solution issue missing: %v", issues)
	}
	if len(issues) != 2 {
		t.Errorf("got %d issues, want 2: %v", len(issues), issues)
	}
}
