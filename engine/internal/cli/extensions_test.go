package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/approval"
	"github.com/Smohan-vcode/O365-Box/engine/internal/extensions"
	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
	"github.com/Smohan-vcode/O365-Box/engine/internal/tui"
)

const extA = "aaaabbbbccccddddeeeeffffgggghhhh" // fictional

// extFake is the Admin site's ExtensionRequests and Events lists.
type extFake struct {
	state     string
	patches   []map[string]any
	events    []map[string]any
	failPatch bool
}

func (f *extFake) serve(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	p := r.URL.Path
	switch {
	case strings.HasSuffix(p, "/items/4") && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "4", "fields": map[string]any{"extension_id": extA, "extension_name": "Example Notes",
			"reason": "client call notes", "requester": "staff-1", "state": f.state}})
	case strings.HasSuffix(p, "/items") && strings.Contains(p, "ExtensionRequests") && r.Method == http.MethodGet:
		v := []any{}
		if f.state == extensions.Requested {
			v = append(v, map[string]any{"id": "4", "fields": map[string]any{"extension_id": extA, "extension_name": "Example Notes", "reason": "client call notes", "requester": "staff-1", "state": f.state}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"value": v})
	case strings.HasSuffix(p, "/items/4/fields") && r.Method == http.MethodPatch:
		if f.failPatch {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":{"code":"accessDenied","message":"denied"}}`)
			return
		}
		f.patches = append(f.patches, body)
		f.state, _ = body["state"].(string)
		_, _ = io.WriteString(w, `{}`)
	case strings.HasSuffix(p, "/lists/Events/items") && r.Method == http.MethodPost:
		f.events = append(f.events, body["fields"].(map[string]any))
		_, _ = io.WriteString(w, `{"id":"1"}`)
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func extTestDeps(t *testing.T, f *extFake) (*extDeps, string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	b, err := os.ReadFile(acmeConfig)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	doc := filepath.Join(dir, "box-config.yaml")
	if err := os.WriteFile(doc, b, 0o600); err != nil {
		t.Fatal(err)
	}
	gc := &graph.Client{HTTP: srv.Client(), Token: func() string { return "t" }}
	return &extDeps{reg: &extensions.Register{G: gc, Base: srv.URL + "/v1.0", SiteID: "s"}, docPath: doc, root: repoRootForTest(t),
		who: "owner-test", dir: dir, now: func() time.Time { return time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC) },
		lookup: func(context.Context, string) (string, error) { return "", errors.New("store not reachable") }}, string(b)
}

func TestExtensionApproveWritesConfigurationThenDecision(t *testing.T) {
	f := &extFake{state: extensions.Requested}
	d, before := extTestDeps(t, f)
	ctx := context.Background()
	r, err := d.pendingRequest(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.approve(ctx, r, true)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(d.docPath)
	if !strings.HasPrefix(string(after), before) {
		t.Error("the approval changed other lines of the configuration")
	}
	l, _ := extensions.ReadLists(after)
	if len(l.Allow) != 1 || l.Allow[0] != extA || len(l.Forced) != 1 {
		t.Fatalf("lists %+v", l)
	}
	if !res.Changed || !strings.Contains(res.Label, "not looked up") {
		t.Errorf("result %+v", res)
	}
	if len(f.patches) != 1 || f.patches[0]["state"] != extensions.Approved || f.patches[0]["decided_by"] != "owner-test" {
		t.Errorf("decision %v", f.patches)
	}
	if len(f.events) != 1 || f.events[0]["action"] != "approve" || f.events[0]["to_state"] != extensions.Approved {
		t.Errorf("events %v", f.events)
	}
	// A decided request is never decided again.
	if _, err := d.pendingRequest(ctx, 4); err == nil || !strings.Contains(err.Error(), "already approved") {
		t.Errorf("second decision: %v", err)
	}
}

func TestExtensionApproveRestoresConfigurationWhenTheDecisionFails(t *testing.T) {
	f := &extFake{state: extensions.Requested, failPatch: true}
	d, before := extTestDeps(t, f)
	r, err := d.pendingRequest(context.Background(), 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.approve(context.Background(), r, false); err == nil {
		t.Fatal("approval succeeded without the decision")
	}
	after, _ := os.ReadFile(d.docPath)
	if string(after) != before || len(f.events) != 0 {
		t.Error("the configuration kept an extension the owner's decision was not recorded for")
	}
}

func TestExtensionRejectNeedsAReason(t *testing.T) {
	f := &extFake{state: extensions.Requested}
	d, before := extTestDeps(t, f)
	r, _ := d.pendingRequest(context.Background(), 4)
	if _, _, err := d.reject(context.Background(), r, "  "); err == nil {
		t.Fatal("rejected without a reason")
	}
	if _, _, err := d.reject(context.Background(), r, "use the company note tool"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(d.docPath)
	if string(after) != before || f.state != extensions.Rejected || f.events[0]["action"] != "reject" {
		t.Errorf("reject: state %s events %v", f.state, f.events)
	}
}

func TestExtensionHomeHooks(t *testing.T) {
	f := &extFake{state: extensions.Requested}
	d, _ := extTestDeps(t, f)
	list, decide := d.homeHooks(context.Background())
	rs, err := list()
	if err != nil || len(rs) != 1 || rs[0].Number != 4 || rs[0].ExtensionID != extA {
		t.Fatalf("requests %v %v", rs, err)
	}
	msg, err := decide(tui.RequestDecision{Number: 4, Approve: true})
	if err != nil || !strings.Contains(msg, "approved") {
		t.Fatalf("decide %q %v", msg, err)
	}
	if rs, _ := list(); len(rs) != 0 {
		t.Errorf("still waiting: %v", rs)
	}
	if _, err := os.Stat(filepath.Join(d.dir, approval.EventsFile)); err == nil {
		t.Error("the Events item waited locally although the tenant took it")
	}
}
