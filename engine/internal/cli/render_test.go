package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/approval"
	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
	"github.com/Smohan-vcode/O365-Box/engine/internal/docs"
	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
	"github.com/Smohan-vcode/O365-Box/engine/internal/plan"
	"github.com/Smohan-vcode/O365-Box/engine/internal/run"
)

const acmeDictionary = "../../../examples/acme/dictionary.yaml"

// fixtureDocs loads the docs package's two test templates.
func fixtureDocs(string) ([]docs.Doc, error) {
	return docs.Load(filepath.Join("..", "docs", "testdata", "repo"))
}

func withFixtureDocs(t *testing.T) {
	old := loadDocs
	loadDocs = fixtureDocs
	t.Cleanup(func() { loadDocs = old })
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestRenderPoliciesCommand(t *testing.T) {
	withFixtureDocs(t)
	out := filepath.Join(t.TempDir(), "preview")
	code, text := box(t, "render", "policies", "--config", acmeConfig, "--dictionary", acmeDictionary, "--out", out)
	if code != ExitOK {
		t.Fatalf("render: %d %s", code, text)
	}
	for _, f := range []string{"POL-T1 Identity and access.docx", "POL-T2 Access review.docx", "index.html"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("%s missing", f)
		}
	}
	// Values the company has not supplied are listed for the owner, never invented.
	for _, want := range []string{"POL-T1 v1.0  POL-T1 Identity and access.docx  (1 value(s) to be completed)", "✓ POL-T2 v1.1",
		"dictionary.role.owner.phone: Phone number"} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
	idx, _ := os.ReadFile(filepath.Join(out, "index.html"))
	if !strings.Contains(string(idx), "POL-T1%20Identity%20and%20access.docx") || !strings.Contains(string(idx), "dictionary.role.owner.phone") || strings.Contains(string(idx), "<script") {
		t.Errorf("index.html = %s", idx)
	}
	if code, text := box(t, "render", "policies", "--config", acmeConfig, "--out", "tmp-preview"); code != ExitValidation || !strings.Contains(text, "inside the repository") {
		t.Errorf("inside the repository: %d %s", code, text)
	}
}

func TestRenderContextValues(t *testing.T) {
	doc, err := config.Load(acmeConfig)
	if err != nil {
		t.Fatal(err)
	}
	var dict map[string]string
	if err := readYAMLFile(acmeDictionary, &dict); err != nil {
		t.Fatal(err)
	}
	c, err := renderContext(repoRootForTest(t), doc, dict)
	if err != nil {
		t.Fatal(err)
	}
	cfg := doc.Config
	v := c.Values
	owner := cfg.Users[0]
	checks := map[string]string{
		"dictionary.company.legal_name":         cfg.Company.LegalName,
		"dictionary.company.registered_address": dict["company.address.street"] + ", " + dict["company.address.city"] + ", " + dict["company.address.postcode"] + ", " + dict["company.address.country"],
		"dictionary.company.notices_email":      dict["company.notices_email"],
		"dictionary.role.owner.name":            owner.GivenName + " " + owner.FamilyName,
		"dictionary.role.owner.email":           owner.Primary + "@" + cfg.Company.PrimaryDomain,
		"generated.mailbox.security":            "security@" + cfg.Company.PrimaryDomain,
	}
	for k, want := range checks {
		if v[k] != want {
			t.Errorf("%s = %q, want %q", k, v[k], want)
		}
	}
	if !strings.Contains(v["generated.frameworks"], "SOC 2") {
		t.Errorf("frameworks = %q", v["generated.frameworks"])
	}
	// Not supplied: left to the owner with what to supply.
	for k, want := range map[string]string{
		"generated.retention_schedule": "confirmation of the retention schedule",
		"generated.approved_ai":        "Copilot and AI features",
	} {
		if v[k] != "" || !strings.Contains(c.Pending[k], want) {
			t.Errorf("%s = %q, pending %q", k, v[k], c.Pending[k])
		}
	}
	// Filled from the rules files acme selects (gb, eu, de), marked as drafts.
	if got := v["jurisdictions.registered"]; got != "United Kingdom (rules not yet reviewed by counsel)" {
		t.Errorf("jurisdictions.registered = %q (%s)", got, c.Pending["jurisdictions.registered"])
	}
	if got := v["jurisdictions.operating"]; !strings.Contains(got, "Germany (rules not yet reviewed by counsel)") {
		t.Errorf("jurisdictions.operating = %q", got)
	}
	for _, want := range []string{"| Applies when |", "| United Kingdom | UK GDPR Art. 33 |", "| European Union (common rules) | NIS2 Art. 23 |", "| Germany | BSI-Gesetz"} {
		if !strings.Contains(v["jurisdictions.notification_deadlines"], want) {
			t.Errorf("notification_deadlines lacks %q: %s", want, v["jurisdictions.notification_deadlines"])
		}
	}
	if _, ok := v["dictionary.role.owner.phone"]; ok {
		t.Error("a phone number was invented")
	}
	// Every non-Dictionary key PLACEHOLDERS.md lists that is company-wide is
	// either filled or says what to supply.
	for _, k := range []string{"generated.frameworks", "generated.retention_schedule", "generated.approved_ai", "generated.mailbox.security",
		"generated.mailbox.privacy", "jurisdictions.registered", "jurisdictions.operating", "jurisdictions.notification_deadlines"} {
		if _, ok := c.Known[k]; !ok {
			t.Errorf("%s is not in PLACEHOLDERS.md", k)
		}
		if c.Values[k] == "" && c.Pending[k] == "" {
			t.Errorf("%s has neither a value nor what to supply", k)
		}
	}
	// A confirmed schedule and an answered AI choice fill them.
	doc.Raw["retention"] = map[string]any{"status": "CONFIRMED", "classes": map[string]any{"tax": map[string]any{"keep": "10y"}}}
	doc.Config.Choices = map[string]string{"ai.copilot": "off"}
	if c, err = renderContext(repoRootForTest(t), doc, dict); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.Values["generated.retention_schedule"], "| tax | 10y |") || !strings.Contains(c.Values["generated.retention_schedule"], "| corporate | permanent |") {
		t.Errorf("retention = %q", c.Values["generated.retention_schedule"])
	}
	if !strings.Contains(c.Values["generated.approved_ai"], "Copilot and AI features: Off until") {
		t.Errorf("approved_ai = %q (%s)", c.Values["generated.approved_ai"], c.Pending["generated.approved_ai"])
	}
}

// TestPoliciesInputCarriesApproval checks the rule "the owner approves
// before it is published": only a complete document whose publish change
// the owner approved carries an approved version, with approver and date.
func TestPoliciesInputCarriesApproval(t *testing.T) {
	withFixtureDocs(t)
	doc, err := config.Load(acmeConfig)
	if err != nil {
		t.Fatal(err)
	}
	var dict map[string]string
	if err := readYAMLFile(acmeDictionary, &dict); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	af, err := approval.Open(filepath.Join(dir, approval.ApprovalsFile))
	if err != nil {
		t.Fatal(err)
	}
	areas := &approval.Areas{}
	s := &session{root: repoRootForTest(t), doc: doc, dictionary: dict, approvals: af, areas: areas}
	in, err := s.buildPoliciesInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	list := in["documents"].([]any)
	if len(list) != 2 || in["library"] != "Policies" || in["dictionary_source"] != "file" {
		t.Fatalf("input = %v", in)
	}
	// POL-T1 has values to be completed: it stays a draft, no publish change.
	t1 := list[0].(map[string]any)
	if _, ok := t1["approve"]; ok || len(t1["missing"].([]any)) != 1 {
		t.Errorf("POL-T1 = %v", t1["missing"])
	}
	needed := in["dictionary_needed"].([]any)
	if len(needed) != 1 || needed[0].(map[string]any)["key"] != "role.owner.phone" {
		t.Errorf("dictionary needed = %v", needed)
	}
	t2 := list[1].(map[string]any)
	ap := t2["approve"].(map[string]any)
	if ap["target"] != "document POL-T2 v1.1" || !strings.HasPrefix(ap["detail"].(string), "approve and publish as PDF (content ") {
		t.Errorf("approve change = %v", ap)
	}
	if _, ok := t2["approval"]; ok {
		t.Error("an approved version without an approval")
	}
	draft, _ := base64.StdEncoding.DecodeString(t2["draft"].(map[string]any)["content"].(string))
	if h, _ := docs.TextHash(draft); h != t2["draft"].(map[string]any)["hash"] {
		t.Error("draft hash does not match its content")
	}

	// The owner approves POL-T2's publish change (as box approve records it).
	step := &plan.Step{ID: PoliciesStep, Title: "Policy documents"}
	c := approval.NewChange(areas, step, run.Change{Action: "update", Target: ap["target"].(string), Detail: ap["detail"].(string)})
	if run.Digest(PoliciesStep, c.Run()) != c.Digest {
		t.Fatal("digest mismatch")
	}
	at := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	if _, err := af.Approve([]approval.Change{c}, "owner-account", "r-1", at); err != nil {
		t.Fatal(err)
	}
	s.approvals, _ = approval.Open(filepath.Join(dir, approval.ApprovalsFile))
	if in, err = s.buildPoliciesInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	list = in["documents"].([]any)
	a, ok := list[1].(map[string]any)["approval"].(map[string]any)
	if !ok || a["by"] != "owner-account" || a["on"] != "2026-11-21" || a["review_due"] != "2027-05-21" || a["digest"] != c.Digest {
		t.Fatalf("approval = %v", a)
	}
	if _, ok := list[0].(map[string]any)["approval"]; ok {
		t.Error("POL-T1 was not approved but carries an approval")
	}
	// Once applied, the approval is history and is not passed again.
	if err := af.Applied(PoliciesStep, []run.Change{c.Run()}, "r-2"); err != nil {
		t.Fatal(err)
	}
	s.approvals, _ = approval.Open(filepath.Join(dir, approval.ApprovalsFile))
	in, _ = s.buildPoliciesInput(context.Background())
	if b, _ := json.Marshal(in); strings.Contains(string(b), "owner-account") {
		t.Error("an applied approval was passed again")
	}
}

func TestReadTenantDictionary(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method %s", r.Method)
		}
		switch {
		case r.URL.Path == "/v1.0/sites/exampletenant.sharepoint.com:/sites/admin":
			_, _ = w.Write([]byte(`{"id":"site-1"}`))
		case r.URL.Path == "/v1.0/sites/site-1/lists/Dictionary/items" && r.URL.Query().Get("page") == "":
			_, _ = w.Write([]byte(`{"value":[{"fields":{"key":"company.telephone","value":"+00 1"}},{"fields":{"key":"company.state","value":""}}],
				"@odata.nextLink":"https://` + r.Host + `/v1.0/sites/site-1/lists/Dictionary/items?page=2"}`))
		case r.URL.Path == "/v1.0/sites/site-1/lists/Dictionary/items":
			_, _ = w.Write([]byte(`{"value":[{"fields":{"key":"company.notices_email","value":"n@x.invalid"}}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"itemNotFound","message":"no"}}`))
		}
	}))
	defer srv.Close()
	gc := &graph.Client{HTTP: srv.Client(), Token: func() string { return "t" }}
	got, err := readTenantDictionary(context.Background(), gc, srv.URL+"/v1.0", "exampletenant.sharepoint.com", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["company.telephone"] != "+00 1" || got["company.notices_email"] != "n@x.invalid" {
		t.Errorf("dictionary = %v", got)
	}
	if _, err := readTenantDictionary(context.Background(), gc, srv.URL+"/v1.0", "exampletenant.sharepoint.com", "nope"); err == nil {
		t.Error("a missing site was not an error")
	}
}
