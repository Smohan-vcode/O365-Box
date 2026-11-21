package docs

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
)

const renderRoot = "../../.."

// fixtures loads testdata/repo: two small templates (POL-T1, POL-T2).
func fixtures(t *testing.T) []Doc {
	t.Helper()
	ds, err := Load(filepath.Join("testdata", "repo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 2 || ds[0].Version == "" || ds[1].Body == "" {
		t.Fatalf("fixtures = %+v", ds)
	}
	return ds
}

func testCatalogue(t *testing.T) *policy.Catalogue {
	t.Helper()
	cat, err := policy.Load(filepath.Join(renderRoot, "policy"), "policy")
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

// Made-up values; nothing here is a real company.
var testValues = map[string]string{
	"dictionary.company.legal_name":   "Testbed Instruments Limited",
	"dictionary.company.trading_name": "Testbed",
	"generated.mailbox.security":      "security@testbed.invalid",
	"jurisdictions.registered":        "Testland",
	"dictionary.role.owner.phone":     "+00 0000 0000",
}

func testResolved() *policy.Result {
	return &policy.Result{
		Status: map[string]string{"IAM-01": policy.StatusEnforced, "IAM-13": policy.StatusProcedure},
		Applied: []policy.AppliedCentre{{ID: "entra", Title: "Entra admin centre", Settings: []policy.AppliedSetting{
			{ID: "ENT-001", Name: "Require MFA for all users", Value: "on", Mode: policy.ModeEnforce, Requirements: []string{"IAM-01"}},
			{ID: "ENT-002", Name: "Block legacy sign-in", Value: "on", Mode: policy.ModeAudit, Requirements: []string{"IAM-03"}},
		}}},
	}
}

func testContext(t *testing.T) Context {
	return Context{Values: testValues, Catalogue: testCatalogue(t), Resolved: testResolved(), Frameworks: []string{"soc2"}}
}

func parts(t *testing.T, docx []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(docx), int64(len(docx)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(b)
	}
	return out
}

func wellFormed(t *testing.T, name, s string) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(s))
	for {
		_, err := d.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("%s is not well-formed XML: %v", name, err)
		}
	}
}

func TestRenderDraft(t *testing.T) {
	ds := fixtures(t)
	r, err := Render(ds[0], testContext(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Testbed Instruments Limited", "pending approval", "version 1.0", "security@testbed.invalid", "Testland",
		"| IAM-01 | Every user signs in with multi-factor authentication for every cloud service. | core | technical | SOC 2",
		"Settings applied by the box |", "| IAM-13 |", "Procedure (this document)", "- Require MFA for all users: on (Entra admin centre)"} {
		if !strings.Contains(r.Markdown, want) {
			t.Errorf("draft lacks %q:\n%s", want, r.Markdown)
		}
	}
	if len(r.Missing) != 0 || strings.Contains(r.Markdown, "{{") || strings.Contains(r.Markdown, MarkerPrefix) {
		t.Errorf("missing = %v", r.Missing)
	}
	if strings.Contains(r.Markdown, "Block legacy sign-in") {
		t.Error("a setting for another requirement is in the summary")
	}
	if r.Status != StatusDraft || r.FileName != "POL-T1 Identity and access.docx" || r.PDFName != "POL-T1 Identity and access.pdf" {
		t.Errorf("draft = %s %s %s", r.Status, r.FileName, r.PDFName)
	}
	p := parts(t, r.DOCX)
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/styles.xml", "word/numbering.xml",
		"word/header1.xml", "word/footer1.xml", "word/_rels/document.xml.rels", "docProps/core.xml"} {
		if _, ok := p[name]; !ok {
			t.Errorf("%s missing", name)
			continue
		}
		wellFormed(t, name, p[name])
	}
	doc := p["word/document.xml"]
	for _, want := range []string{`<w:pStyle w:val="Heading1"/>`, `<w:pStyle w:val="Heading2"/>`, `<w:numId w:val="1"/>`, `<w:numId w:val="2"/>`,
		`<w:tbl>`, `<w:tblHeader/>`, `<w:b/>`, `<w:i/>`, "Testbed Instruments Limited", `r:id="rId3"`, `r:id="rId4"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("document.xml lacks %s", want)
		}
	}
	if !strings.Contains(p["word/header1.xml"], "POL-T1 Identity and access · Version 1.0 · Draft") {
		t.Errorf("header = %s", p["word/header1.xml"])
	}
	if !strings.Contains(p["word/footer1.xml"], "Draft awaiting approval by owner · review every 12 months") {
		t.Errorf("footer = %s", p["word/footer1.xml"])
	}
	if h, _ := TextHash(r.DOCX); h != r.Hash || len(h) != 64 {
		t.Errorf("hash %s, TextHash %s", r.Hash, h)
	}
	again, _ := Render(ds[0], testContext(t), nil)
	if !bytes.Equal(again.DOCX, r.DOCX) || again.Source != r.Source {
		t.Error("rendering is not deterministic")
	}
	c := testContext(t)
	c.Values = Merge(testValues, map[string]string{"dictionary.company.legal_name": "Other Name Limited"})
	if other, _ := Render(ds[0], c, nil); other.Source == r.Source {
		t.Error("a changed value kept the source")
	}
}

func TestRenderApproved(t *testing.T) {
	ds := fixtures(t)
	draft, _ := Render(ds[1], testContext(t), nil)
	on := time.Date(2026, 11, 21, 15, 4, 5, 0, time.UTC)
	r, err := Render(ds[1], testContext(t), &Approval{By: "owner-account", On: on})
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != draft.Source {
		t.Error("approval changed the source: the plan would see a new draft")
	}
	if r.Hash == draft.Hash {
		t.Error("the approved file has the draft's text")
	}
	if r.Status != StatusApproved || r.ApprovedBy != "owner-account" || r.ApprovedOn.Format(DateISO) != "2026-11-21" ||
		r.ReviewDue.Format(DateISO) != "2027-05-21" {
		t.Errorf("approved = %+v", r)
	}
	p := parts(t, r.DOCX)
	if !strings.Contains(p["word/footer1.xml"], "Approved by owner on 21 November 2026 · review due 21 May 2027") {
		t.Errorf("footer = %s", p["word/footer1.xml"])
	}
	if !strings.Contains(p["word/header1.xml"], "Version 1.1 · Approved") {
		t.Errorf("header = %s", p["word/header1.xml"])
	}
	if _, err := Render(ds[1], testContext(t), &Approval{On: on}); err == nil {
		t.Error("an approval without an approver was accepted")
	}
}

// TestMissingValuesAreMarked: a value the company has not supplied is a
// visible marker, listed, and blocks approval; never invented, never blank.
func TestMissingValuesAreMarked(t *testing.T) {
	ds := fixtures(t)
	known, err := Placeholders(renderRoot)
	if err != nil {
		t.Fatal(err)
	}
	c := testContext(t)
	c.Known = known
	c.Values = map[string]string{"dictionary.company.legal_name": "Testbed Instruments Limited", "dictionary.role.owner.phone": " ",
		"generated.mailbox.security": "security@testbed.invalid"}
	c.Pending = map[string]string{"jurisdictions.registered": "the jurisdiction rules file defaults/jurisdictions/xx.yaml"}
	r, err := Render(ds[0], c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Missing) != 2 || r.Missing[0].Key != "dictionary.role.owner.phone" || r.Missing[1].Key != "jurisdictions.registered" {
		t.Fatalf("missing = %+v", r.Missing)
	}
	if !strings.Contains(r.Markdown, "[to be completed: Phone number at which the `owner` role holder can be reached in an incident]") ||
		!strings.Contains(r.Markdown, "[to be completed: the jurisdiction rules file defaults/jurisdictions/xx.yaml]") {
		t.Errorf("markers:\n%s", r.Markdown)
	}
	if !strings.Contains(parts(t, r.DOCX)["word/footer1.xml"], "2 value(s) to be completed") {
		t.Error("footer does not say values are to be completed")
	}
	if _, err := Render(ds[0], c, &Approval{By: "x", On: time.Now()}); err == nil {
		t.Error("a document with values to be completed was approved")
	}
	// Supplying the value changes the content.
	c.Values["dictionary.role.owner.phone"] = "+00 1"
	if r2, _ := Render(ds[0], c, nil); r2.Source == r.Source || len(r2.Missing) != 1 {
		t.Error("supplying a value did not change the document")
	}
	// No resolved selection: the settings summary is a marker.
	c.Resolved = nil
	if r3, _ := Render(ds[0], c, nil); !strings.Contains(r3.Markdown, MarkerPrefix+"Plain-words summary") {
		t.Errorf("settings summary without a selection:\n%s", r3.Markdown)
	}
	// A key PLACEHOLDERS.md does not list is a template error.
	d := ds[1]
	d.Body = "{{ company.legal_name }} {{generated.nonsense}} {{dictionary.company.trading_name}}"
	_, err = Render(d, c, nil)
	var u *UnfilledError
	if !errors.As(err, &u) || strings.Join(u.Keys, ",") != "company.legal_name,generated.nonsense" {
		t.Errorf("err = %v", err)
	}
	if _, err := RenderAll([]Doc{d, ds[0]}, c); strings.Join(UnfilledKeys(err), ",") != "company.legal_name,generated.nonsense" {
		t.Errorf("RenderAll err = %v", err)
	}
	d = ds[1]
	d.Requirements = []string{"NOPE-99"}
	if _, err := Render(d, testContext(t), nil); err == nil || !strings.Contains(err.Error(), "NOPE-99") {
		t.Errorf("err = %v", err)
	}
}

// TestRepositoryTemplatesRender renders every real template with no
// company values: each renders, every gap is a marker and listed.
func TestRepositoryTemplatesRender(t *testing.T) {
	ds, err := Load(renderRoot)
	if err != nil {
		t.Fatal(err)
	}
	known, err := Placeholders(renderRoot)
	if err != nil {
		t.Fatal(err)
	}
	c := Context{Known: known, Catalogue: testCatalogue(t), Resolved: testResolved(), Frameworks: []string{"soc2"}}
	rs, err := RenderAll(ds, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if strings.Contains(r.Markdown, "{{") {
			t.Errorf("%s: a placeholder is left", r.Doc.ID)
		}
		if strings.Count(r.Markdown, MarkerPrefix) < len(r.Missing) {
			t.Errorf("%s: %d missing, fewer markers", r.Doc.ID, len(r.Missing))
		}
		parts(t, r.DOCX)
	}
}

func TestParseMarkdown(t *testing.T) {
	md := "# Title\n\nFirst line\nsecond line.\n\n- one **bold** and *it*\n  more of one\n  - nested `code`\n- [site](https://example.invalid)\n\n" +
		"1. a\n2. b\n\nText.\n\n1. again\n\n---\n\n| A | B |\n|---|:-:|\n| x \\| y | z |\n\n### Small\n#### Smaller\n"
	bs := ParseMarkdown(md)
	got := PlainText(bs)
	want := "Title\nFirst line second line.\n- one bold and it more of one\n- nested code\n- site (https://example.invalid)\n- a\n- b\nText.\n- again\nA | B\nx | y | z\nSmall\nSmaller\n"
	if got != want {
		t.Fatalf("plain text:\n%q\nwant\n%q", got, want)
	}
	li := bs[2].(*ListItem)
	if li.Ordered || li.Level != 0 || !li.Runs[1].Bold || !li.Runs[3].Italic {
		t.Errorf("list item = %+v", li)
	}
	if n := bs[3].(*ListItem); n.Level != 1 || !n.Runs[1].Code {
		t.Errorf("nested = %+v", n)
	}
	a, b, again := bs[5].(*ListItem), bs[6].(*ListItem), bs[8].(*ListItem)
	if !a.Ordered || a.List != 1 || b.List != 1 || again.List != 2 {
		t.Errorf("numbered lists %d %d %d: a new list restarts", a.List, b.List, again.List)
	}
	if tb := bs[9].(*Table); len(tb.Rows) != 1 || tb.Rows[0][0] != "x | y" {
		t.Errorf("table = %+v", tb)
	}
	docx, err := (&Document{Title: "t", Blocks: bs}).DOCX()
	if err != nil {
		t.Fatal(err)
	}
	p := parts(t, docx)
	wellFormed(t, "document.xml", p["word/document.xml"])
	if !strings.Contains(p["word/numbering.xml"], `<w:num w:numId="3">`) {
		t.Error("the second numbered list has no numbering of its own")
	}
}

func rezip(t *testing.T, docx []byte, change map[string]string) []byte {
	t.Helper()
	p := parts(t, docx)
	for k, v := range change {
		p[k] = v
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for k, v := range p {
		w, _ := zw.Create(k)
		_, _ = io.WriteString(w, v)
	}
	_ = zw.Close()
	return buf.Bytes()
}

func TestTextHashIgnoresProperties(t *testing.T) {
	r, err := Render(fixtures(t)[1], testContext(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	props := rezip(t, r.DOCX, map[string]string{"docProps/core.xml": "<x>changed</x>", "customXml/item1.xml": "<PolicyId>POL-T2</PolicyId>"})
	if h, _ := TextHash(props); h != r.Hash {
		t.Error("a property change changed the text hash")
	}
	p := parts(t, r.DOCX)
	edited := rezip(t, r.DOCX, map[string]string{"word/document.xml": strings.Replace(p["word/document.xml"], "Every quarter", "Every year", 1)})
	if h, _ := TextHash(edited); h == r.Hash {
		t.Error("an edit kept the text hash")
	}
	if _, err := TextHash([]byte("not a zip")); err == nil {
		t.Error("a file that is not a .docx was hashed")
	}
}

func TestCompanyValuesAndRecords(t *testing.T) {
	v := CompanyValues("dictionary.company", map[string]any{"legal_name": "A Ltd", "domain_aliases": []any{"a.invalid", "b.invalid"},
		"address": map[string]any{"city": "X"}, "none": nil})
	if v["dictionary.company.legal_name"] != "A Ltd" || v["dictionary.company.domain_aliases"] != "a.invalid, b.invalid" || v["dictionary.company.address.city"] != "X" {
		t.Errorf("values = %v", v)
	}
	if _, ok := v["dictionary.company.none"]; ok {
		t.Error("a null value became a key")
	}
	if m := Merge(map[string]string{"a": "1"}, map[string]string{"a": " ", "b": ""}); m["a"] != "1" {
		t.Errorf("a blank value replaced a value: %v", m)
	}
	rec := RecordsByRequirement(fixtures(t))
	if strings.Join(rec["IAM-13"], ",") != "ControlTasks:access_review quarterly" || len(rec["IAM-01"]) != 0 {
		t.Errorf("records = %v", rec)
	}
}

func TestFixturesHaveNoCompanyValues(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(renderRoot, "examples", "acme", "dictionary.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var dict map[string]string
	_ = yaml.Unmarshal(b, &dict)
	for _, d := range fixtures(t) {
		for k, v := range dict {
			if len(v) > 4 && strings.Contains(d.Body, v) {
				t.Errorf("%s holds the example value of %s", d.ID, k)
			}
		}
	}
}
