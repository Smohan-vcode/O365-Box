package audit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
)

// policyGraph serves a fictional tenant's Compliance and Admin sites:
// POL-03 approved (IAM-13), POL-15 a draft (GOV-02), PRC-01 approved
// (INC-01, INC-02), POL-11 approved but past its review (SUP-01); one
// access review done in the period, one risk assessment done before it.
func policyGraph(t *testing.T, refuse bool) (*httptest.Server, *[]string) {
	var mu sync.Mutex
	var seen []string
	docs := `{"value":[
	 {"id":"1","fields":{"FileLeafRef":"POL-03 Identity and access.pdf","PolicyId":"POL-03","PolicyVersion":"1.0","PolicyStatus":"Approved","Requirements":"IAM-01, IAM-13","ReviewDue":"2027-06-01T00:00:00Z","ApprovedBy":"owner-account","ApprovedOn":"2026-06-01T00:00:00Z","BoxHash":"aa"},"driveItem":{"name":"POL-03 Identity and access.pdf","lastModifiedDateTime":"2026-06-01T10:00:00Z","file":{"hashes":{"quickXorHash":"qx1"}}}},
	 {"id":"2","fields":{"FileLeafRef":"POL-15 Risk assessment.docx","PolicyId":"POL-15","PolicyVersion":"1.0","PolicyStatus":"Draft","Requirements":"GOV-02","BoxHash":"bb"}},
	 {"id":"3","fields":{"FileLeafRef":"PRC-01 Incident response plan.pdf","PolicyId":"PRC-01","PolicyVersion":"1.0","PolicyStatus":"Approved","Requirements":"INC-01, INC-02","ReviewDue":"2027-03-01","ApprovedBy":"owner-account","ApprovedOn":"2026-03-01"}},
	 {"id":"4","fields":{"FileLeafRef":"POL-11 Vendor accounts.pdf","PolicyId":"POL-11","PolicyVersion":"1.0","PolicyStatus":"Approved","Requirements":"SUP-01","ReviewDue":"2026-01-01","ApprovedBy":"owner-account","ApprovedOn":"2025-01-01"}},
	 {"id":"5","fields":{"FileLeafRef":"Notes.docx"}}]}`
	controls := `{"value":[{"id":"7","fields":{"control_id":"C-04","evidence_source":"task.access_review"}},{"id":"8","fields":{"control_id":"C-13","evidence_source":"task.risk_assessment"}}]}`
	tasks := `{"value":[
	 {"id":"1","fields":{"task_id":"T-1","control_idLookupId":"7","state":"DONE","due_on":"2026-09-30","closed_at":"2026-09-28T10:00:00Z"}},
	 {"id":"2","fields":{"task_id":"T-2","control_idLookupId":"8","state":"DONE","due_on":"2025-06-30","closed_at":"2025-06-30T10:00:00Z"}},
	 {"id":"3","fields":{"task_id":"T-3","control_idLookupId":"7","state":"OPEN","due_on":"2026-12-31"}}]}`
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body := ""
		switch r.URL.Path {
		case "/v1.0/sites/fictional.sharepoint.com:/sites/compliance":
			if refuse {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"code":"accessDenied","message":"no"}}`))
				return
			}
			body = `{"id":"cs"}`
		case "/v1.0/sites/cs/lists/Policies/items":
			body = docs
		case "/v1.0/sites/fictional.sharepoint.com:/sites/admin":
			body = `{"id":"as"}`
		case "/v1.0/sites/as/lists/Controls/items":
			body = controls
		case "/v1.0/sites/as/lists/ControlTasks/items":
			body = tasks
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"itemNotFound","message":"` + r.URL.Path + `"}}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func policyReader(t *testing.T, refuse bool) (*Reader, *[]string) {
	srv, seen := policyGraph(t, refuse)
	p, err := ParsePeriod("2026-01-01..2026-11-21", testNow)
	if err != nil {
		t.Fatal(err)
	}
	gc := &graph.Client{HTTP: ReadOnlyClient(srv.Client()), Token: func() string { return "t" }}
	return &Reader{Graph: gc, V1: srv.URL + "/v1.0", Beta: srv.URL + "/beta", Period: p, Now: func() time.Time { return testNow }}, seen
}

var testLocation = PolicyLocation{Host: "fictional.sharepoint.com", PoliciesSite: "compliance", Library: "Policies", AdminSite: "admin",
	Records: map[string][]string{"INC-02": {"ControlTasks:incident_register on event"}, "IAM-13": {"ControlTasks:access_review quarterly"}}}

func TestReadPoliciesAndAssess(t *testing.T) {
	r, seen := policyReader(t, false)
	recs, invs := r.ReadPolicies(context.Background(), testLocation)
	if !recs.DocsRead || !recs.TasksRead || len(recs.Documents) != 4 || len(recs.Tasks) != 3 {
		t.Fatalf("records = %+v", recs)
	}
	for _, s := range *seen {
		if !strings.HasPrefix(s, "GET ") {
			t.Errorf("not a read: %s", s)
		}
	}
	if len(invs) != 2 || invs[0].ID != "policy_documents" || invs[0].Count != 0 && len(invs[0].Rows) != 4 || invs[0].Rows[0]["quickxor_hash"] != "qx1" {
		t.Errorf("document inventory = %+v", invs[0])
	}
	if invs[1].Summary != "3 task(s), 1 done in the period" {
		t.Errorf("tasks summary = %q", invs[1].Summary)
	}

	cat, _ := catalogue(t)
	fw := cat.Framework("soc2")
	a := AssessWith(cat, fw, map[string]*SettingRead{}, invs, recs)
	want := map[string]struct{ status, doc, gap string }{
		"IAM-13": {Met, DocInPlace, ""},
		"GOV-02": {DocumentNeeded, DocDraft, "POL-15 v1.0 is a draft awaiting owner approval"},
		"INC-01": {PartlyMet, DocInPlace, "procedure in place (PRC-01 v1.0 (approved 2026-03-01, review due 2027-03-01)), no record of it being done in the period 2026-01-01..2026-11-21 (ControlTasks:incident_exercise)"},
		"INC-02": {Met, DocInPlace, ""},
		"SUP-01": {DocumentNeeded, DocReviewDue, "past its review date"},
		"AWR-01": {DocumentNeeded, DocNone, "no approved document in compliance/Policies lists AWR-01"},
	}
	for id, w := range want {
		rs := a.Requirements[id]
		if rs == nil {
			t.Errorf("%s not assessed (not in soc2?)", id)
			continue
		}
		if rs.Status != w.status || rs.Document != w.doc || (w.gap == "" && rs.Gap != "") || !strings.Contains(rs.Gap, w.gap) {
			t.Errorf("%s = %s / %s / %q; want %s / %s / %q", id, rs.Status, rs.Document, rs.Gap, w.status, w.doc, w.gap)
		}
		if (rs.Document == DocInPlace) == rs.NeedsDoc {
			t.Errorf("%s: document %s but needs doc %v", id, rs.Document, rs.NeedsDoc)
		}
	}
	if recs := a.Requirements["IAM-13"].Records; len(recs) != 1 || recs[0].Done != 1 || recs[0].Frequency != "quarterly" {
		t.Errorf("IAM-13 records = %+v", recs)
	}
	for _, g := range a.Gaps() {
		if g.Requirement.ID == "IAM-13" || g.Requirement.ID == "INC-02" {
			t.Errorf("%s is met but listed as a gap", g.Requirement.ID)
		}
	}
	// The control of IAM-13 no longer lists it as a document needed.
	for _, c := range a.Controls {
		for _, d := range c.Documents {
			if d == "IAM-13" {
				t.Errorf("%s still needs a document for IAM-13", c.ID)
			}
		}
	}

	// The pack carries the document list and the tasks as evidence.
	dir := filepath.Join(t.TempDir(), "pack")
	p := &Pack{Tenant: "t", ReadBy: "test", Period: r.Period, ReadAt: testNow, Framework: fw, Assessment: a, Inventories: invs, Settings: map[string]*SettingRead{}}
	if _, err := Write(dir, p); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, c := range a.Controls {
		for _, f := range c.Evidence {
			if strings.HasSuffix(f, "/policy_documents.csv") {
				b, _ := os.ReadFile(filepath.Join(dir, f))
				found = strings.Contains(string(b), "owner-account") && strings.Contains(string(b), "POL-15")
			}
		}
	}
	if !found {
		t.Error("no control has the document list (versions, approvals, hashes) as evidence")
	}
}

func TestReadPoliciesNotRead(t *testing.T) {
	r, _ := policyReader(t, true)
	recs, invs := r.ReadPolicies(context.Background(), testLocation)
	if recs.DocsRead || invs[0].Status != NotRead || !strings.Contains(invs[0].Reason, "access refused") {
		t.Fatalf("docs = %+v", invs[0])
	}
	cat, _ := catalogue(t)
	a := AssessWith(cat, cat.Framework("soc2"), map[string]*SettingRead{}, invs, recs)
	rs := a.Requirements["IAM-13"]
	if rs.Status != DocumentNeeded || rs.Document != DocLibNotRead || !strings.Contains(rs.Gap, "policy library not read") {
		t.Errorf("IAM-13 = %+v", rs)
	}
	// No host: nothing is asked.
	recs, invs = r.ReadPolicies(context.Background(), PolicyLocation{})
	if recs.DocsRead || invs[0].Status != NotRead {
		t.Errorf("no host = %+v", invs[0])
	}
}

func TestParseRecord(t *testing.T) {
	for in, want := range map[string]RecordRef{
		"ControlTasks:access_review quarterly":           {List: "ControlTasks", Task: "access_review", Frequency: "quarterly"},
		"ControlTasks:break_glass_test every six months": {List: "ControlTasks", Task: "break_glass_test", Frequency: "every six months"},
		"task.restore_test":                              {List: "ControlTasks", Task: "restore_test"},
		"JoinerChecklists:joiner and leaver on event":    {List: "JoinerChecklists", Task: "joiner", Frequency: "and leaver on event"},
	} {
		if got := ParseRecord(in); got != want {
			t.Errorf("%s = %+v", in, got)
		}
	}
}
