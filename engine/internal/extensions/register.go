package extensions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/approval"
	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
)

// Register reads and writes the ExtensionRequests list and the Events list
// in the Admin site over Microsoft Graph, with the pipeline app's token
// (Sites.FullControl.All, base bundle).
type Register struct {
	G      *graph.Client
	Base   string // Graph base, e.g. https://graph.microsoft.com/v1.0
	SiteID string
}

// OpenRegister finds the Admin site by its host and URL name.
func OpenRegister(ctx context.Context, gc *graph.Client, base, host, adminURL string) (*Register, error) {
	var site struct {
		ID string `json:"id"`
	}
	if err := gc.Get(ctx, base+"/sites/"+host+":/sites/"+url.PathEscape(adminURL)+"?$select=id", &site); err != nil {
		return nil, fmt.Errorf("Admin site: %w", err)
	}
	return &Register{G: gc, Base: base, SiteID: site.ID}, nil
}

func (r *Register) list(name string) string { return r.Base + "/sites/" + r.SiteID + "/lists/" + name }

type item struct {
	ID              string         `json:"id"`
	CreatedDateTime string         `json:"createdDateTime"`
	Fields          map[string]any `json:"fields"`
	CreatedBy       struct {
		User struct {
			DisplayName string `json:"displayName"`
			Email       string `json:"email"`
		} `json:"user"`
	} `json:"createdBy"`
}

func str(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return strings.TrimSpace(v)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func toRequest(it item) Request {
	n, _ := strconv.Atoi(it.ID)
	f := it.Fields
	r := Request{Item: n, ExtensionID: strings.ToLower(str(f, "extension_id")), Name: str(f, "extension_name"), StoreName: str(f, "store_name"),
		Reason: str(f, "reason"), Requester: str(f, "requester"), State: str(f, "state"), DecidedBy: str(f, "decided_by"),
		DecidedAt: str(f, "decided_at"), DecisionReason: str(f, "decision_reason"), Created: it.CreatedDateTime}
	if b, ok := f["install_for_everyone"].(bool); ok {
		r.InstallForEveryone = b
	}
	if r.Name == "" {
		r.Name = str(f, "Title")
	}
	if r.Requester == "" {
		// A request made in the list's form: the work account that added it.
		r.Requester = it.CreatedBy.User.Email
		if r.Requester == "" {
			r.Requester = it.CreatedBy.User.DisplayName
		}
	}
	if r.State == "" {
		r.State = Requested
	}
	return r
}

// Requests reads every request.
func (r *Register) Requests(ctx context.Context) ([]Request, error) {
	var out []Request
	next := r.list(List) + "/items?$expand=fields&$top=500"
	for next != "" {
		var page struct {
			Value []item `json:"value"`
			Next  string `json:"@odata.nextLink"`
		}
		if err := r.G.Get(ctx, next, &page); err != nil {
			if ge, ok := err.(*graph.Error); ok && ge.Status == http.StatusNotFound {
				return nil, fmt.Errorf("the %s list does not exist yet: run box apply sharepoint.register", List)
			}
			return nil, fmt.Errorf("%s: %w", List, err)
		}
		for _, it := range page.Value {
			out = append(out, toRequest(it))
		}
		next = page.Next
	}
	return out, nil
}

// Request reads one request by its number.
func (r *Register) Request(ctx context.Context, n int) (*Request, error) {
	var it item
	if err := r.G.Get(ctx, r.list(List)+"/items/"+strconv.Itoa(n)+"?$expand=fields", &it); err != nil {
		if ge, ok := err.(*graph.Error); ok && ge.Status == http.StatusNotFound {
			return nil, fmt.Errorf("no request %d in %s", n, List)
		}
		return nil, err
	}
	x := toRequest(it)
	return &x, nil
}

// Submit adds a request (box extensions request) and returns its number.
func (r *Register) Submit(ctx context.Context, q Request) (int, error) {
	if err := CheckID(q.ExtensionID); err != nil {
		return 0, err
	}
	if strings.TrimSpace(q.Reason) == "" {
		return 0, fmt.Errorf("a request needs a reason")
	}
	f := map[string]any{"Title": q.Name, "extension_id": q.ExtensionID, "extension_name": q.Name, "reason": q.Reason,
		"requester": q.Requester, "install_for_everyone": q.InstallForEveryone, "state": Requested}
	if q.Name == "" {
		f["Title"] = q.ExtensionID
	}
	var it item
	if err := r.G.Do(ctx, http.MethodPost, r.list(List)+"/items", map[string]any{"fields": f}, &it); err != nil {
		return 0, fmt.Errorf("add request: %w", err)
	}
	n, _ := strconv.Atoi(it.ID)
	return n, nil
}

// Decision is the owner's answer to a request.
type Decision struct {
	State     string // Approved or Rejected
	By        string
	At        time.Time
	Reason    string // required for a rejection
	StoreName string // the store's name, when it was looked up
	Everyone  bool   // approved to be installed for everyone
}

// Decide records the decision on the request item.
func (r *Register) Decide(ctx context.Context, n int, d Decision) error {
	f := map[string]any{"state": d.State, "decided_by": d.By, "decided_at": d.At.UTC().Format(time.RFC3339)}
	if d.Reason != "" {
		f["decision_reason"] = d.Reason
	}
	if d.StoreName != "" {
		f["store_name"] = d.StoreName
	}
	if d.State == Approved {
		f["install_for_everyone"] = d.Everyone
	}
	if err := r.G.Do(ctx, http.MethodPatch, r.list(List)+"/items/"+strconv.Itoa(n)+"/fields", f, nil); err != nil {
		return fmt.Errorf("record the decision on request %d: %w", n, err)
	}
	return nil
}

// AddEvent adds one Events item (R10: created, never changed).
func (r *Register) AddEvent(ctx context.Context, ev *approval.EventItem) error {
	f := map[string]any{"Title": ev.Entity + " " + ev.EntityID + " " + ev.Action, "event_id": ev.EventID,
		"at": ev.At.UTC().Format(time.RFC3339), "actor": ev.Actor, "entity": ev.Entity, "entity_id": ev.EntityID, "action": ev.Action}
	if ev.FromState != "" {
		f["from_state"] = ev.FromState
	}
	if ev.ToState != "" {
		f["to_state"] = ev.ToState
	}
	if len(ev.Detail) > 0 {
		b, err := json.Marshal(ev.Detail)
		if err != nil {
			return err
		}
		f["detail"] = string(b)
	}
	if err := r.G.Do(ctx, http.MethodPost, r.list("Events")+"/items", map[string]any{"fields": f}, nil); err != nil {
		return fmt.Errorf("Events item: %w", err)
	}
	return nil
}
