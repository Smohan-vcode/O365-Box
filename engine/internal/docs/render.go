package docs

// Rendering (T9-01): a template's placeholders are filled from the
// company's values and the catalogue, and the result is written as a Word
// document. A value the company has not supplied is never invented: it is
// rendered as a visible "[to be completed: <what>]" marker, listed in
// Rendered.Missing, and the document cannot be approved until it is filled.
// A placeholder that PLACEHOLDERS.md does not list is an error.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
)

// Document statuses in the Policies library.
const (
	StatusDraft    = "Draft"
	StatusApproved = "Approved"
)

// DefaultReviewMonths is the review period of a template that names none.
const DefaultReviewMonths = 12

// DateText is how dates read inside a document; DateISO is how they are
// stored in columns.
const (
	DateText = "2 January 2006"
	DateISO  = "2006-01-02"
)

// MarkerPrefix starts the visible marker of a value still to be supplied.
const MarkerPrefix = "[to be completed: "

// Context is what a template is filled from.
type Context struct {
	// Values holds company-wide placeholder values by full key
	// ("dictionary.company.legal_name", "generated.frameworks",
	// "jurisdictions.registered"). An absent or blank value is rendered as
	// a marker. The per-document generated values (date, version,
	// requirement_table, settings_summary, next_review, review_months, ...)
	// are computed by Render and need not be here.
	Values map[string]string
	// Pending says, for a key without a value, what the owner must supply
	// when that differs from the key's description in PLACEHOLDERS.md
	// (for example "a rules file defaults/jurisdictions/gb.yaml").
	Pending map[string]string
	// Known are the keys PLACEHOLDERS.md documents, with their
	// descriptions. A key outside it is an error. nil accepts any key in
	// the three namespaces (tests).
	Known map[string]string
	// Catalogue gives each requirement's statement, tier, type and
	// framework references.
	Catalogue *policy.Catalogue
	// Resolved is the company's resolved policy selection: how each
	// requirement is enforced and which settings are applied. nil renders
	// the settings summary as a marker.
	Resolved *policy.Result
	// Frameworks are the selected framework ids (policy.frameworks), for
	// the requirement table's references.
	Frameworks []string
}

// Approval is the owner's approval of a document version.
type Approval struct {
	By string    // who approved (the approval record's approver)
	On time.Time // when
}

// Missing is a value the owner must supply before a document can be approved.
type Missing struct {
	Key  string `json:"key"`  // the placeholder key
	What string `json:"what"` // what to supply, in plain words
}

// Rendered is one filled document.
type Rendered struct {
	Doc      Doc
	Status   string // Draft | Approved
	Markdown string // the body as filled
	// Missing lists the values still to be supplied; while it is not
	// empty the document stays a draft.
	Missing []Missing
	// Source identifies the content whatever its status: the SHA-256 of the
	// id, version, title and the body filled as a draft. It changes when
	// the template, a company value or the resolved selection changes.
	Source     string
	DOCX       []byte
	Hash       string // TextHash(DOCX)
	FileName   string // the .docx file name in the library
	PDFName    string // the published copy's file name
	ApprovedBy string
	ApprovedOn time.Time
	ReviewDue  time.Time
}

// UnfilledError lists placeholders a document uses that are not known keys:
// a template mistake, never rendered blank.
type UnfilledError struct {
	Doc  string
	Keys []string
}

func (e *UnfilledError) Error() string {
	return fmt.Sprintf("%s: unknown placeholder(s): %s", e.Doc, strings.Join(e.Keys, ", "))
}

// UnfilledKeys returns every unknown key in err (one UnfilledError or
// several joined), sorted and without repeats.
func UnfilledKeys(err error) []string {
	var out []string
	var walk func(error)
	walk = func(e error) {
		var u *UnfilledError
		if errors.As(e, &u) {
			out = append(out, u.Keys...)
		}
		if j, ok := e.(interface{ Unwrap() []error }); ok {
			for _, x := range j.Unwrap() {
				walk(x)
			}
		}
	}
	walk(err)
	sort.Strings(out)
	return compact(out)
}

func compact(s []string) []string {
	var out []string
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

var fillKeyRe = regexp.MustCompile(`^(dictionary|generated|jurisdictions)\.[A-Za-z0-9_][A-Za-z0-9_.\-]*$`)

// Marker is the visible text of a value still to be supplied.
func Marker(what string) string {
	return MarkerPrefix + strings.TrimSuffix(strings.TrimSpace(what), ".") + "]"
}

// fill replaces every placeholder. It returns the values still missing and
// the keys that are not known.
func fill(body string, c Context, own map[string]string) (string, []Missing, []string) {
	var missing []Missing
	var unknown []string
	seen := map[string]bool{}
	out := placeholderRe.ReplaceAllStringFunc(body, func(m string) string {
		key := placeholderRe.FindStringSubmatch(m)[1]
		desc, known := c.Known[key]
		if !fillKeyRe.MatchString(key) || (c.Known != nil && !known) {
			unknown = append(unknown, key)
			return m
		}
		v, ok := own[key]
		if !ok {
			v = c.Values[key]
		}
		if strings.TrimSpace(v) != "" {
			return v
		}
		what := c.Pending[key]
		if what == "" {
			what = desc
		}
		if what == "" {
			what = key
		}
		if !seen[key] {
			seen[key] = true
			missing = append(missing, Missing{Key: key, What: what})
		}
		return Marker(what)
	})
	sort.Slice(missing, func(i, j int) bool { return missing[i].Key < missing[j].Key })
	sort.Strings(unknown)
	return out, missing, compact(unknown)
}

// reviewMonths is the document's review period.
func reviewMonths(d Doc) int {
	if d.ReviewMonths > 0 {
		return d.ReviewMonths
	}
	return DefaultReviewMonths
}

var unsafeFile = regexp.MustCompile(`[^A-Za-z0-9 ._()-]+`)

// FileBase is the document's file name without extension: "<id> <title>".
func FileBase(d Doc) string {
	s := strings.Join(strings.Fields(unsafeFile.ReplaceAllString(d.ID+" "+d.Title, " ")), " ")
	return strings.Trim(s, " .")
}

// Render fills a document: as a draft, or as approved when a is set. A
// document with values still missing cannot be approved.
func Render(d Doc, c Context, a *Approval) (*Rendered, error) {
	if d.ID == "" || d.Version == "" {
		return nil, fmt.Errorf("%s: a document needs an id and a version", d.File)
	}
	own, err := generated(d, c)
	if err != nil {
		return nil, err
	}
	draft, missing, unknown := fill(d.Body, c, own)
	if len(unknown) > 0 {
		return nil, &UnfilledError{Doc: d.ID, Keys: unknown}
	}
	sum := sha256.Sum256([]byte(d.ID + "\n" + d.Version + "\n" + d.Title + "\n" + draft))
	r := &Rendered{Doc: d, Status: StatusDraft, Markdown: draft, Missing: missing, Source: hex.EncodeToString(sum[:]),
		FileName: FileBase(d) + ".docx", PDFName: FileBase(d) + ".pdf"}
	approver := orText(d.ApproverRole, "the owner")
	footer := fmt.Sprintf("Draft awaiting approval by %s · review every %d months", approver, reviewMonths(d))
	if len(missing) > 0 {
		footer = fmt.Sprintf("Draft: %d value(s) to be completed before approval by %s", len(missing), approver)
	}
	if a != nil {
		if len(missing) > 0 {
			return nil, fmt.Errorf("%s: cannot be approved while %d value(s) are to be completed", d.ID, len(missing))
		}
		if a.By == "" || a.On.IsZero() {
			return nil, fmt.Errorf("%s: an approval needs who and when", d.ID)
		}
		on := time.Date(a.On.Year(), a.On.Month(), a.On.Day(), 0, 0, 0, 0, time.UTC)
		r.Status, r.ApprovedBy, r.ApprovedOn, r.ReviewDue = StatusApproved, a.By, on, on.AddDate(0, reviewMonths(d), 0)
		own["generated.date"], own["generated.status"] = on.Format(DateText), StatusApproved
		own["generated.next_review"] = r.ReviewDue.Format(DateText)
		r.Markdown, _, _ = fill(d.Body, c, own)
		footer = fmt.Sprintf("Approved by %s on %s · review due %s", approver, on.Format(DateText), r.ReviewDue.Format(DateText))
	}
	blocks := ParseMarkdown(r.Markdown)
	if h, ok := first(blocks).(*Heading); !ok || h.Level != 1 {
		blocks = append([]Block{&Heading{Level: 1, Runs: []Run{{Text: d.Title}}}}, blocks...)
	}
	doc := &Document{Title: d.ID + " " + d.Title, Header: fmt.Sprintf("%s %s · Version %s · %s", d.ID, d.Title, d.Version, r.Status),
		Footer: footer, Blocks: blocks}
	if r.DOCX, err = doc.DOCX(); err != nil {
		return nil, err
	}
	if r.Hash, err = TextHash(r.DOCX); err != nil {
		return nil, err
	}
	return r, nil
}

func first(b []Block) Block {
	if len(b) == 0 {
		return nil
	}
	return b[0]
}

func orText(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// RenderAll renders every document as a draft. The error joins every
// document's problem, so all unknown keys are listed at once.
func RenderAll(ds []Doc, c Context) ([]*Rendered, error) {
	var out []*Rendered
	var errs []error
	for _, d := range ds {
		r, err := Render(d, c, nil)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, r)
	}
	return out, errors.Join(errs...)
}

// generated are a document's own {{generated.*}} values, as a draft.
func generated(d Doc, c Context) (map[string]string, error) {
	months := reviewMonths(d)
	g := map[string]string{
		"generated.id": d.ID, "generated.title": d.Title, "generated.version": d.Version, "generated.status": StatusDraft,
		"generated.date":          "pending approval",
		"generated.next_review":   fmt.Sprintf("%d months after approval", months),
		"generated.review_months": strconv.Itoa(months),
		"generated.owner_role":    d.OwnerRole, "generated.approver_role": d.ApproverRole,
	}
	if c.Catalogue != nil {
		t, err := RequirementTable(d, c.Catalogue, c.Resolved, c.Frameworks)
		if err != nil {
			return nil, err
		}
		g["generated.requirement_table"] = t
		if c.Resolved != nil {
			g["generated.settings_summary"] = SettingsSummary(d, c.Catalogue, c.Resolved)
		}
	}
	return g, nil
}

// RequirementTable is a Markdown table of the document's requirements:
// id, statement, tier, type, the selected frameworks' references and how
// each is enforced for this company.
func RequirementTable(d Doc, cat *policy.Catalogue, res *policy.Result, frameworks []string) (string, error) {
	if len(d.Requirements) == 0 {
		return "", fmt.Errorf("%s: no requirements in the front matter for the requirement table", d.ID)
	}
	var b strings.Builder
	b.WriteString("| Requirement | Statement | Tier | Type | Frameworks | How it is enforced here |\n| --- | --- | --- | --- | --- | --- |\n")
	for _, id := range d.Requirements {
		r := cat.Requirement(id)
		if r == nil {
			return "", fmt.Errorf("%s: requirement %s is not in the catalogue", d.ID, id)
		}
		var refs []string
		for _, fw := range frameworks {
			if l := r.Frameworks[fw]; len(l) > 0 {
				name := fw
				if f := cat.Framework(fw); f != nil && f.Title != "" {
					name = f.Title
				}
				refs = append(refs, name+": "+strings.Join(l, ", "))
			}
		}
		ref := strings.Join(refs, "; ")
		if ref == "" {
			ref = "none selected"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", r.ID, cellText(r.Statement), r.Tier, r.Type, cellText(ref), Enforcement(r, res))
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

func cellText(s string) string {
	return strings.ReplaceAll(strings.Join(strings.Fields(s), " "), "|", `\|`)
}

// Enforcement says in plain words how a requirement is enforced.
func Enforcement(r *policy.Requirement, res *policy.Result) string {
	st := ""
	if res != nil {
		st = res.Status[r.ID]
	}
	switch st {
	case policy.StatusEnforced:
		return "Settings applied by the box"
	case policy.StatusEnforcedProcedure:
		return "Settings applied by the box, and this procedure"
	case policy.StatusProcedure:
		return "Procedure (this document)"
	case policy.StatusAddon:
		return "Needs a paid add-on; not applied"
	case policy.StatusNotSelected:
		return "Not selected for this company"
	case policy.StatusException:
		return "Owner exception"
	}
	switch r.Type {
	case policy.TypeTechnical:
		return "Settings"
	case policy.TypeProcedural:
		return "Procedure (this document)"
	}
	return "Settings and procedure"
}

// SettingsSummary lists, as Markdown bullets, the applied settings that
// serve the document's requirements, then the settings not applied and why.
func SettingsSummary(d Doc, cat *policy.Catalogue, res *policy.Result) string {
	want := map[string]bool{}
	for _, id := range d.Requirements {
		want[id] = true
	}
	serves := func(reqs []string) bool {
		for _, id := range reqs {
			if want[id] {
				return true
			}
		}
		return false
	}
	var lines []string
	for _, c := range res.Applied {
		for _, s := range c.Settings {
			if serves(s.Requirements) {
				mode := ""
				if s.Mode != "" && s.Mode != policy.ModeEnforce {
					mode = ", " + s.Mode + " only"
				}
				lines = append(lines, fmt.Sprintf("- %s: %s (%s%s)", s.Name, s.Value, c.Title, mode))
			}
		}
	}
	var notApplied []string
	for _, n := range res.NotApplied {
		if s := cat.Setting(n.ID); s != nil && serves(s.Requirements) {
			notApplied = append(notApplied, fmt.Sprintf("- Not applied: %s (%s)", n.Name, n.Reason))
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "- No settings are applied for this document's requirements; it is carried out as a procedure.")
	}
	return strings.Join(append(lines, notApplied...), "\n")
}

// CompanyValues flattens a configuration section into keys:
// CompanyValues("dictionary.company", raw["company"]) gives
// "dictionary.company.legal_name" and so on. Lists of scalars are joined
// with ", ".
func CompanyValues(prefix string, v any) map[string]string {
	out := map[string]string{}
	var walk func(string, any)
	walk = func(p string, x any) {
		switch t := x.(type) {
		case map[string]any:
			for k, vv := range t {
				walk(p+"."+k, vv)
			}
		case []any:
			var parts []string
			for _, e := range t {
				switch e.(type) {
				case map[string]any, []any:
					return
				}
				if e != nil {
					parts = append(parts, fmt.Sprint(e))
				}
			}
			out[p] = strings.Join(parts, ", ")
		case nil:
		default:
			out[p] = fmt.Sprint(t)
		}
	}
	walk(prefix, v)
	return out
}

// Merge returns the values of every map; a later map wins over an earlier
// one, except that a blank value never replaces a value.
func Merge(ms ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range ms {
		for k, v := range m {
			if strings.TrimSpace(v) == "" {
				if _, ok := out[k]; ok {
					continue
				}
			}
			out[k] = v
		}
	}
	return out
}

// RecordsByRequirement returns, per requirement id, the recurring records
// ("List:thing frequency") the documents' front matter names for it.
func RecordsByRequirement(ds []Doc) map[string][]string {
	out := map[string][]string{}
	for _, d := range ds {
		for _, r := range d.Requirements {
			out[r] = append(out[r], d.Records...)
		}
	}
	for k, v := range out {
		sort.Strings(v)
		out[k] = compact(v)
	}
	return out
}

// ApproveChange is the planned change that publishes a document version
// once the owner approves it: the sharepoint.policies step plans it for a
// draft whose content is current and complete, and the approval of exactly
// this change (its digest covers the content) is what lets the step mark
// the document Approved and publish it.
func ApproveChange(r *Rendered) (target, detail string) {
	return "document " + r.Doc.ID + " v" + r.Doc.Version, "approve and publish as PDF (content " + r.Source[:12] + ")"
}
