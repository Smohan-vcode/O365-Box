package audit

// The company's written policies and their records (T9-01, T9-02, SPEC
// §26): the audit reads the Compliance site's Policies library and the
// ControlTasks list over Graph, GET only. A requirement that needs a
// written procedure has its document in place when an Approved document
// listing it is in the library with its review not yet due; its recurring
// records ("ControlTasks:<task> <frequency>" in the templates' front
// matter, and the catalogue's task.<task> evidence) are evidenced only by
// ControlTasks items done in the audit period.

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
)

// Columns of the Policies library, as the sharepoint.policies step writes them.
const (
	ColPolicyID     = "PolicyId"
	ColVersion      = "PolicyVersion"
	ColStatus       = "PolicyStatus"
	ColRequirements = "Requirements"
	ColReviewDue    = "ReviewDue"
	ColApprovedBy   = "ApprovedBy"
	ColApprovedOn   = "ApprovedOn"
	ColBoxHash      = "BoxHash"
	ColBoxSource    = "BoxSource"
)

// Document states of a requirement.
const (
	DocInPlace      = "document in place"
	DocDraft        = "draft awaiting owner approval"
	DocReviewDue    = "approved document past its review date"
	DocNone         = "document needed"
	DocLibNotRead   = "policy library not read"
	recordsListName = "ControlTasks"
)

// PolicyLocation is where the box keeps the documents and the register.
type PolicyLocation struct {
	Host         string // <name>.sharepoint.com
	PoliciesSite string // site URL name holding the library (compliance)
	Library      string // Policies
	AdminSite    string // site URL name holding the register lists (admin)
	// Records are the templates' recurring records by requirement id
	// (docs.RecordsByRequirement).
	Records map[string][]string
}

// PolicyDocument is one file in the Policies library.
type PolicyDocument struct {
	File         string   `json:"file"`
	PolicyID     string   `json:"policy_id"`
	Version      string   `json:"version"`
	Status       string   `json:"status"`
	Requirements []string `json:"requirements"`
	ReviewDue    string   `json:"review_due,omitempty"`
	ApprovedBy   string   `json:"approved_by,omitempty"`
	ApprovedOn   string   `json:"approved_on,omitempty"`
	BoxHash      string   `json:"box_hash,omitempty"`
	QuickXorHash string   `json:"quickxor_hash,omitempty"`
	Modified     string   `json:"modified,omitempty"`
	URL          string   `json:"url,omitempty"`
}

// ControlTask is one ControlTasks item.
type ControlTask struct {
	TaskID   string `json:"task_id"`
	Task     string `json:"task"` // the control's task key (access_review)
	State    string `json:"state"`
	DueOn    string `json:"due_on,omitempty"`
	ClosedAt string `json:"closed_at,omitempty"`
}

// Records is what the audit read of the documents and the tasks.
type Records struct {
	Location     PolicyLocation
	AsOf         time.Time
	Period       Period
	DocsRead     bool
	DocsReason   string
	Documents    []PolicyDocument
	TasksRead    bool
	TasksReason  string
	Tasks        []ControlTask
	inventoryIDs []string
}

// RecordRef is a recurring record a requirement needs.
type RecordRef struct {
	List      string `json:"list"`
	Task      string `json:"task"`
	Frequency string `json:"frequency,omitempty"`
	Done      int    `json:"done_in_period"`
	Checked   bool   `json:"checked"` // false: kept in a list the audit does not read
}

// ParseRecord reads "ControlTasks:access_review quarterly"; the catalogue's
// "task.access_review" is the same record with no frequency.
func ParseRecord(s string) RecordRef {
	s = strings.TrimSpace(s)
	if t, ok := strings.CutPrefix(s, "task."); ok {
		return RecordRef{List: recordsListName, Task: t}
	}
	list, rest, ok := strings.Cut(s, ":")
	if !ok {
		return RecordRef{Task: s}
	}
	task, freq, _ := strings.Cut(strings.TrimSpace(rest), " ")
	return RecordRef{List: list, Task: task, Frequency: strings.TrimSpace(freq)}
}

func (r RecordRef) String() string {
	s := r.List + ":" + r.Task
	if r.Frequency != "" {
		s += " " + r.Frequency
	}
	return s
}

// onEvent reports whether a record is kept only when something happens:
// none in the period is not a gap.
func (r RecordRef) onEvent() bool { return strings.Contains(r.Frequency, "on event") }

func fieldStr(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (r *Reader) siteID(ctx context.Context, host, site string, inv *Inventory) (string, error) {
	p := "/sites/" + host + ":/sites/" + url.PathEscape(site) + "?$select=id,webUrl"
	inv.Queries = append(inv.Queries, "GET "+p)
	var s struct {
		ID string `json:"id"`
	}
	err := r.get(ctx, p, &s)
	return s.ID, err
}

// ReadPolicies reads the Policies library and the ControlTasks list. It
// returns the records for the assessment and their two inventories (the
// document list with versions, approvals and hashes; the tasks).
func (r *Reader) ReadPolicies(ctx context.Context, loc PolicyLocation) (*Records, []*Inventory) {
	r.defaults()
	now := r.Now().UTC()
	recs := &Records{Location: loc, AsOf: now, Period: r.Period, inventoryIDs: []string{"policy_documents", "control_tasks"}}
	docs := &Inventory{ID: "policy_documents", Title: "Policy documents in the Compliance site (versions, approvals, hashes)", Status: Read, ReadAt: now}
	tasks := &Inventory{ID: "control_tasks", Title: "Recurring control tasks done (ControlTasks)", Status: Read, ReadAt: now}
	const needs = "Sites.Read.All and read access to the Compliance and Admin sites"
	if loc.Host == "" {
		for _, inv := range []*Inventory{docs, tasks} {
			inv.Status, inv.Reason, inv.Summary = NotRead, "the tenant's SharePoint host is not known (organisation not read)", "not read"
		}
		recs.DocsReason, recs.TasksReason = docs.Reason, tasks.Reason
		return recs, []*Inventory{docs, tasks}
	}

	// The documents.
	if id, err := r.siteID(ctx, loc.Host, loc.PoliciesSite, docs); err != nil {
		fail(docs, err, needs)
	} else {
		p := "/sites/" + id + "/lists/" + url.PathEscape(loc.Library) + "/items?$expand=fields,driveItem($select=id,name,webUrl,lastModifiedDateTime,file)&$top=200"
		docs.Queries = append(docs.Queries, "GET "+p)
		items, trunc, err := r.list(ctx, p, 2000)
		docs.Truncated = trunc
		if err != nil {
			fail(docs, err, needs)
		}
		for _, it := range items {
			f, _ := it["fields"].(map[string]any)
			di, _ := it["driveItem"].(map[string]any)
			d := PolicyDocument{File: fieldStr(f, "FileLeafRef"), PolicyID: fieldStr(f, ColPolicyID), Version: fieldStr(f, ColVersion),
				Status: fieldStr(f, ColStatus), Requirements: splitList(fieldStr(f, ColRequirements)), ReviewDue: dateOnly(fieldStr(f, ColReviewDue)),
				ApprovedBy: fieldStr(f, ColApprovedBy), ApprovedOn: dateOnly(fieldStr(f, ColApprovedOn)), BoxHash: fieldStr(f, ColBoxHash),
				QuickXorHash: str(di, "file.hashes.quickXorHash"), Modified: str(di, "lastModifiedDateTime"), URL: str(di, "webUrl")}
			if d.File == "" {
				d.File = str(di, "name")
			}
			if d.PolicyID == "" {
				continue // not a box document
			}
			recs.Documents = append(recs.Documents, d)
			docs.Rows = append(docs.Rows, map[string]any{"file": d.File, "policy_id": d.PolicyID, "version": d.Version, "status": d.Status,
				"requirements": strings.Join(d.Requirements, ", "), "review_due": d.ReviewDue, "approved_by": d.ApprovedBy, "approved_on": d.ApprovedOn,
				"box_hash": d.BoxHash, "quickxor_hash": d.QuickXorHash, "modified": d.Modified})
		}
		sort.Slice(recs.Documents, func(i, j int) bool { return recs.Documents[i].File < recs.Documents[j].File })
		recs.DocsRead = docs.Status != NotRead
	}
	if !recs.DocsRead {
		recs.DocsReason = docs.Reason
	}
	approved := 0
	for _, d := range recs.Documents {
		if d.Status == "Approved" {
			approved++
		}
	}
	docs.Summary = fmt.Sprintf("%d document(s), %d approved", len(recs.Documents), approved)

	// The tasks, joined to their control's task key.
	if id, err := r.siteID(ctx, loc.Host, loc.AdminSite, tasks); err != nil {
		fail(tasks, err, needs)
	} else {
		pc := "/sites/" + id + "/lists/Controls/items?$expand=fields($select=control_id,evidence_source)&$top=500"
		pt := "/sites/" + id + "/lists/ControlTasks/items?$expand=fields&$top=500"
		tasks.Queries = append(tasks.Queries, "GET "+pc, "GET "+pt)
		controls, _, err := r.list(ctx, pc, 0)
		if err != nil {
			fail(tasks, err, needs)
		}
		keyOf := map[string]string{}
		for _, c := range controls {
			f, _ := c["fields"].(map[string]any)
			k := strings.TrimPrefix(fieldStr(f, "evidence_source"), "task.")
			if k == "" {
				k = fieldStr(f, "control_id")
			}
			keyOf[fieldStr(c, "id")] = k
		}
		items, trunc, err := r.list(ctx, pt, 5000)
		tasks.Truncated = trunc
		if err != nil {
			fail(tasks, err, needs)
		}
		for _, it := range items {
			f, _ := it["fields"].(map[string]any)
			t := ControlTask{TaskID: fieldStr(f, "task_id"), Task: keyOf[fieldStr(f, "control_idLookupId")], State: fieldStr(f, "state"),
				DueOn: dateOnly(fieldStr(f, "due_on")), ClosedAt: fieldStr(f, "closed_at")}
			recs.Tasks = append(recs.Tasks, t)
			tasks.Rows = append(tasks.Rows, map[string]any{"task_id": t.TaskID, "task": t.Task, "state": t.State, "due_on": t.DueOn, "closed_at": t.ClosedAt})
		}
		recs.TasksRead = tasks.Status != NotRead
	}
	if !recs.TasksRead {
		recs.TasksReason = tasks.Reason
	}
	tasks.Summary = fmt.Sprintf("%d task(s), %d done in the period", len(recs.Tasks), recs.doneInPeriod(""))
	if docs.Status == NotRead {
		docs.Summary = "not read"
	}
	if tasks.Status == NotRead {
		tasks.Summary = "not read"
	}
	return recs, []*Inventory{docs, tasks}
}

func dateOnly(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// doneInPeriod counts the tasks for key ("" for all) done in the period.
func (recs *Records) doneInPeriod(key string) int {
	n := 0
	for _, t := range recs.Tasks {
		match := key == "" || t.Task == key || strings.HasPrefix(t.TaskID, key)
		if !match || !strings.EqualFold(t.State, "DONE") {
			continue
		}
		when := t.ClosedAt
		if when == "" {
			when = t.DueOn
		}
		at, err := time.Parse("2006-01-02", dateOnly(when))
		if err == nil && !at.Before(recs.Period.From) && at.Before(recs.Period.end()) {
			n++
		}
	}
	return n
}

// documentsFor sorts the box documents listing a requirement by state.
func (recs *Records) documentsFor(id string) (inPlace, drafts, overdue []PolicyDocument) {
	asOf := recs.AsOf.Format("2006-01-02")
	for _, d := range recs.Documents {
		if !contains(d.Requirements, id) {
			continue
		}
		switch {
		case d.Status == "Approved" && d.ReviewDue > asOf:
			inPlace = append(inPlace, d)
		case d.Status == "Approved":
			overdue = append(overdue, d)
		default:
			drafts = append(drafts, d)
		}
	}
	return
}

func docNames(ds []PolicyDocument) string {
	seen := map[string]bool{}
	var out []string
	for _, d := range ds {
		s := d.PolicyID + " v" + d.Version
		if d.Status == "Approved" {
			s += " (approved " + d.ApprovedOn + ", review due " + d.ReviewDue + ")"
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return strings.Join(out, ", ")
}

// records lists the recurring records a requirement needs, with how many
// were done in the period.
func (recs *Records) records(r *policy.Requirement) []RecordRef {
	var all []string
	if strings.HasPrefix(r.Evidence, "task.") {
		all = append(all, r.Evidence)
	}
	all = append(all, recs.Location.Records[r.ID]...)
	seen := map[string]int{}
	var out []RecordRef
	for _, s := range all {
		ref := ParseRecord(s)
		key := ref.List + ":" + ref.Task
		if i, ok := seen[key]; ok {
			if out[i].Frequency == "" {
				out[i].Frequency = ref.Frequency
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, ref)
	}
	for i := range out {
		if out[i].List == recordsListName && recs.TasksRead {
			out[i].Checked = true
			out[i].Done = recs.doneInPeriod(out[i].Task)
		}
	}
	return out
}

// applyDocuments sets a requirement that needs a written procedure from
// the documents and records read. It runs after the technical assessment;
// rs.Status is the settings' status for a mixed requirement.
func (recs *Records) applyDocuments(rs *RequirementStatus, r *policy.Requirement) {
	rs.Inventories = append(rs.Inventories, recs.inventoryIDs...)
	if !recs.DocsRead {
		rs.Document = DocLibNotRead
		rs.Gap = strings.TrimPrefix(rs.Gap+"; policy library not read: "+recs.DocsReason, "; ")
		return
	}
	inPlace, drafts, overdue := recs.documentsFor(r.ID)
	rs.Records = recs.records(r)
	tier := tierWeight[r.Tier]
	var docGap string
	switch {
	case len(inPlace) > 0:
		rs.Document, rs.Documents = DocInPlace, docNames(inPlace)
	case len(overdue) > 0:
		rs.Document, rs.Documents = DocReviewDue, docNames(overdue)
		docGap = "the approved document " + docNames(overdue) + " is past its review date"
	case len(drafts) > 0:
		rs.Document, rs.Documents = DocDraft, docNames(drafts)
		docGap = "document " + docNames(drafts) + " is a draft awaiting owner approval"
	default:
		rs.Document = DocNone
		docGap = "no approved document in " + recs.Location.PoliciesSite + "/" + recs.Location.Library + " lists " + r.ID
	}
	if rs.Document != DocInPlace {
		// Unchanged rule: a written, approved procedure is needed.
		rs.NeedsDoc = true
		if r.Type == policy.TypeProcedural {
			rs.Gap = docGap + "; " + rs.Gap
		} else {
			rs.Gap = strings.TrimPrefix(rs.Gap+"; "+docGap, "; ")
		}
		return
	}
	// The document is in place: what is left is proof the procedure runs.
	rs.NeedsDoc = false
	var missing, unread []string
	for _, ref := range rs.Records {
		switch {
		case ref.List == recordsListName && !recs.TasksRead:
			unread = append(unread, ref.String())
		case !ref.Checked || ref.onEvent() || ref.Done > 0:
		default:
			missing = append(missing, ref.String())
		}
	}
	techMet := r.Type == policy.TypeProcedural || rs.Status == Met
	techGap := ""
	if r.Type != policy.TypeProcedural && rs.Status != Met {
		techGap = rs.Gap
		techGap = strings.TrimSuffix(techGap, "; and a written, approved procedure ("+docWhat(r)+")")
	}
	var gaps []string
	if techGap != "" {
		gaps = append(gaps, techGap)
	}
	switch {
	case len(missing) > 0:
		gaps = append(gaps, "procedure in place ("+rs.Documents+"), no record of it being done in the period "+recs.Period.String()+" ("+strings.Join(missing, ", ")+")")
		if techMet {
			rs.Status = PartlyMet
		}
		rs.Fix = "Fix with Company in a Box: the recurring task in ControlTasks is created by the scheduled control-task flow; record each run (state DONE, closed_at) so the period shows it."
	case len(unread) > 0:
		gaps = append(gaps, "procedure in place ("+rs.Documents+"); whether it was done in the period could not be read: "+recs.TasksReason)
		if techMet {
			rs.Status = NotRead
		}
	default:
		if techMet {
			rs.Status, rs.Fix = Met, ""
		}
	}
	rs.Gap = strings.Join(gaps, "; ")
	if techMet {
		rs.Risk = statusRisk[rs.Status] * tier
	}
}
