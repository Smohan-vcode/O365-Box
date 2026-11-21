package extensions

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/approval"
	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
)

// Fictional extension ids (32 letters a to p).
const (
	idA = "aaaabbbbccccddddeeeeffffgggghhhh"
	idB = "ppppoooonnnnmmmmllllkkkkjjjjiiii"
)

func TestValidID(t *testing.T) {
	for _, ok := range []string{idA, idB} {
		if !ValidID(ok) {
			t.Errorf("%s should be valid", ok)
		}
	}
	for _, bad := range []string{"", "short", strings.ToUpper(idA), "aaaabbbbccccddddeeeeffffgggghhhq", idA + "a", "aaaabbbbccccddddeeeeffffgggghhh1"} {
		if ValidID(bad) || CheckID(bad) == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

const doc = `version: 1
tenant:
  environment: test   # keep this comment
modules:
  devices: true
choices:
  devices.browser_extensions: approved
`

func TestSetListsAppendsAndReplaces(t *testing.T) {
	l, changed := Lists{}.Approve(idA, false)
	if !changed || len(l.Allow) != 1 || len(l.Forced) != 0 {
		t.Fatalf("approve: %+v %v", l, changed)
	}
	b, err := SetLists([]byte(doc), l)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), doc) {
		t.Fatalf("other lines changed:\n%s", b)
	}
	got, err := ReadLists(b)
	if err != nil || len(got.Allow) != 1 || got.Allow[0] != idA || len(got.Forced) != 0 {
		t.Fatalf("read back %+v %v\n%s", got, err, b)
	}
	// A second approval, for everyone, replaces the section in place.
	l2, changed := got.Approve(idB, true)
	if !changed {
		t.Fatal("no change")
	}
	b2, err := SetLists(b, l2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b2), "devices:\n") != 1 || !strings.HasPrefix(string(b2), doc) {
		t.Fatalf("section not replaced in place:\n%s", b2)
	}
	got2, _ := ReadLists(b2)
	if strings.Join(got2.Allow, ",") != idA+","+idB || strings.Join(got2.Forced, ",") != idB {
		t.Fatalf("lists %+v", got2)
	}
	// The same approval again changes nothing.
	if _, changed := got2.Approve(idB, true); changed {
		t.Error("approving twice changed the lists")
	}
	// A section in the middle keeps what follows it.
	mid := "version: 1\ndevices:\n  browser:\n    extension_allowlist: [" + idA + "]\nchoices: {}\n"
	b3, err := SetLists([]byte(mid), Lists{Allow: []string{idA, idB}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(b3), "choices: {}\n") || !strings.HasPrefix(string(b3), "version: 1\ndevices:\n") {
		t.Fatalf("middle section:\n%s", b3)
	}
}

func TestSetListsRefuses(t *testing.T) {
	if _, err := SetLists([]byte(doc), Lists{Allow: []string{"not-an-id"}}); err == nil {
		t.Error("an invalid id was written")
	}
	other := "devices:\n  windows:\n    minimum: x\n"
	if _, err := SetLists([]byte(other), Lists{Allow: []string{idA}}); err == nil {
		t.Error("a devices key the box does not write would be dropped")
	}
}

func TestLabelAndPending(t *testing.T) {
	if (Request{StoreName: "Store name", Name: "x"}).Label() != "Store name" {
		t.Error("store name first")
	}
	if l := (Request{Name: "Grammar helper", ExtensionID: idA}).Label(); !strings.Contains(l, "not looked up") {
		t.Errorf("label %q", l)
	}
	p := Pending([]Request{{Item: 3, State: Requested}, {Item: 1, State: Approved}, {Item: 2}})
	if len(p) != 2 || p[0].Item != 2 || p[1].Item != 3 {
		t.Errorf("pending %+v", p)
	}
}

func TestLookupName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, idA) {
			_, _ = io.WriteString(w, `{"name":" Example Notes ","crxId":"`+idA+`"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	old := StoreURL
	StoreURL = srv.URL + "/addons/getproductdetailsbycrxid/"
	defer func() { StoreURL = old }()
	n, err := LookupName(context.Background(), srv.Client(), idA)
	if err != nil || n != "Example Notes" {
		t.Errorf("lookup %q %v", n, err)
	}
	if _, err := LookupName(context.Background(), srv.Client(), idB); err == nil {
		t.Error("an unknown id was named")
	}
	if _, err := LookupName(context.Background(), srv.Client(), "bad"); err == nil {
		t.Error("a bad id was sent")
	}
}

// fakeSite serves the Admin site's lists over a Graph-shaped API.
type fakeSite struct {
	items   map[string]map[string]any
	patches []map[string]any
	events  []map[string]any
	created []map[string]any
	failAdd bool
}

func (f *fakeSite) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/v1.0/sites/host.example:/sites/"):
			_, _ = io.WriteString(w, `{"id":"site1"}`)
		case r.Method == http.MethodGet && p == "/v1.0/sites/site1/lists/ExtensionRequests/items":
			var v []any
			for id, f := range f.items {
				v = append(v, map[string]any{"id": id, "fields": f, "createdBy": map[string]any{"user": map[string]any{"displayName": "Pat Example", "email": "pat@acme.example"}}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"value": v})
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/v1.0/sites/site1/lists/ExtensionRequests/items/"):
			id := strings.TrimPrefix(p, "/v1.0/sites/site1/lists/ExtensionRequests/items/")
			it, ok := f.items[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"error":{"code":"itemNotFound","message":"no"}}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "fields": it})
		case r.Method == http.MethodPatch && strings.HasSuffix(p, "/fields"):
			f.patches = append(f.patches, body)
			id := strings.TrimSuffix(strings.TrimPrefix(p, "/v1.0/sites/site1/lists/ExtensionRequests/items/"), "/fields")
			for k, v := range body {
				f.items[id][k] = v
			}
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodPost && p == "/v1.0/sites/site1/lists/ExtensionRequests/items":
			f.created = append(f.created, body["fields"].(map[string]any))
			f.items["9"] = body["fields"].(map[string]any)
			_, _ = io.WriteString(w, `{"id":"9"}`)
		case r.Method == http.MethodPost && p == "/v1.0/sites/site1/lists/Events/items":
			if f.failAdd {
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, `{"error":{"code":"accessDenied","message":"no"}}`)
				return
			}
			f.events = append(f.events, body["fields"].(map[string]any))
			_, _ = io.WriteString(w, `{"id":"1"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, p)
			w.WriteHeader(http.StatusBadRequest)
		}
	})
}

func TestRegister(t *testing.T) {
	fs := &fakeSite{items: map[string]map[string]any{
		"4": {"extension_id": idA, "extension_name": "Example Notes", "reason": "Notes for client calls", "state": "requested", "install_for_everyone": true},
		"5": {"Title": "Something", "extension_id": idB, "reason": "x", "state": "rejected", "decided_by": "owner"},
	}}
	srv := httptest.NewTLSServer(fs.handler(t))
	defer srv.Close()
	gc := &graph.Client{HTTP: srv.Client(), Token: func() string { return "t" }}
	ctx := context.Background()
	reg, err := OpenRegister(ctx, gc, srv.URL+"/v1.0", "host.example", "admin")
	if err != nil {
		t.Fatal(err)
	}
	rs, err := reg.Requests(ctx)
	if err != nil || len(rs) != 2 {
		t.Fatalf("requests %v %v", rs, err)
	}
	p := Pending(rs)
	if len(p) != 1 || p[0].Item != 4 || !p[0].InstallForEveryone || p[0].Requester != "pat@acme.example" {
		t.Fatalf("pending %+v", p)
	}
	if _, err := reg.Request(ctx, 77); err == nil || !strings.Contains(err.Error(), "no request 77") {
		t.Errorf("missing request: %v", err)
	}
	at := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	if err := reg.Decide(ctx, 4, Decision{State: Approved, By: "owner", At: at, StoreName: "Example Notes", Everyone: true}); err != nil {
		t.Fatal(err)
	}
	if got := fs.patches[0]; got["state"] != Approved || got["decided_at"] != "2026-11-21T12:00:00Z" || got["install_for_everyone"] != true || got["store_name"] != "Example Notes" {
		t.Errorf("patch %v", got)
	}
	ev := &approval.EventItem{EventID: "ev-1", At: at, Actor: "owner", Entity: List, EntityID: "4", Action: "approve", FromState: Requested, ToState: Approved, Detail: map[string]any{"extension_id": idA}}
	if err := reg.AddEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if e := fs.events[0]; e["event_id"] != "ev-1" || e["entity"] != List || !strings.Contains(e["detail"].(string), idA) {
		t.Errorf("event %v", e)
	}
	n, err := reg.Submit(ctx, Request{ExtensionID: idB, Name: "Other", Reason: "needed", Requester: "staff-1"})
	if err != nil || n != 9 || fs.created[0]["state"] != Requested {
		t.Errorf("submit %d %v %v", n, err, fs.created)
	}
	if _, err := reg.Submit(ctx, Request{ExtensionID: "bad", Reason: "x"}); err == nil {
		t.Error("a bad id was submitted")
	}
}
