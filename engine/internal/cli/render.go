package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Smohan-vcode/O365-Box/engine/internal/audit"
	"github.com/Smohan-vcode/O365-Box/engine/internal/docs"
	"github.com/Smohan-vcode/O365-Box/engine/internal/events"
	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
)

// PoliciesStep is the sharepoint step that saves the policy documents in
// the Compliance site's Policies library (T9-01, T9-02).
const PoliciesStep = "sharepoint.policies"

// PoliciesLibrary is the library the documents are saved in.
const PoliciesLibrary = "Policies"

// loadDocs loads the policy templates (tests replace it).
var loadDocs = docs.Load

func newRenderCmd(g *globals) *cobra.Command {
	c := &cobra.Command{Use: "render", Short: "Render company documents from their templates"}
	var cfgPath, dictPath, out string
	pc := &cobra.Command{
		Use:   "policies --config <file> --out <folder>",
		Short: "Render the policy documents as Word files to a folder outside the repository, for preview",
		Long: `Fill every template in defaults/policies/ with the company's values (the
configuration document's company section and the Dictionary values in
--dictionary) and the catalogue (requirement table, applied settings), and
write each as a Word document plus index.html to --out, a new or empty folder
outside the repository. Nothing is sent to the tenant: the sharepoint.policies
step saves the documents in the Compliance site's Policies library as drafts,
and publishes each one once the owner approves it (box plan --proposal,
box approve, box up).

A placeholder without a value is an error that names it; nothing is left
blank.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return renderPolicies(g, cmd.OutOrStdout(), cfgPath, dictPath, out)
		},
	}
	pc.Flags().StringVar(&cfgPath, "config", "", "configuration document")
	pc.Flags().StringVar(&dictPath, "dictionary", "", "Dictionary values (key: value), until they are read from the tenant")
	pc.Flags().StringVar(&out, "out", "", "new or empty folder outside the repository")
	c.AddCommand(pc)
	return c
}

func renderPolicies(g *globals, out io.Writer, cfgPath, dictPath, outDir string) error {
	root, err := g.root()
	if err != nil {
		return err
	}
	dir, err := audit.CheckOutDir(root, outDir)
	if err != nil {
		return usageError(strings.Replace(err.Error(), "the pack describes a company", "the documents hold company values", 1))
	}
	doc, err := loadValidConfig(root, cfgPath, out)
	if err != nil {
		return err
	}
	var dict map[string]string
	if dictPath != "" {
		if err := readYAMLFile(dictPath, &dict); err != nil {
			return err
		}
	}
	ctx, err := renderContext(root, doc, dict)
	if err != nil {
		return err
	}
	ds, err := loadDocs(root)
	if err != nil {
		return err
	}
	rendered, err := docs.RenderAll(ds, ctx)
	if err != nil {
		for _, e := range splitErrors(err) {
			fmt.Fprintln(out, "  ✗ "+e.Error())
		}
		msg := fmt.Sprintf("%d document(s) could not be rendered", len(ds)-len(rendered))
		if keys := docs.UnfilledKeys(err); len(keys) > 0 {
			msg += "; placeholders not in " + docs.PlaceholdersFile + ": " + strings.Join(keys, ", ")
		}
		return validationError(msg)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, r := range rendered {
		if err := os.WriteFile(filepath.Join(dir, r.FileName), r.DOCX, 0o600); err != nil {
			return err
		}
	}
	idx, err := renderIndex(rendered)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), idx, 0o600); err != nil {
		return err
	}
	if g.asJSON {
		var list []map[string]any
		for _, r := range rendered {
			list = append(list, map[string]any{"id": r.Doc.ID, "version": r.Doc.Version, "file": r.FileName, "source": r.Source, "hash": r.Hash,
				"to_be_completed": r.Missing})
		}
		return writeJSON(out, map[string]any{"command": "render policies", "out": dir, "documents": list})
	}
	values := map[string]string{}
	for _, r := range rendered {
		mark := "✓"
		extra := ""
		if len(r.Missing) > 0 {
			mark, extra = "…", fmt.Sprintf("  (%d value(s) to be completed)", len(r.Missing))
		}
		fmt.Fprintf(out, "  %s %s v%s  %s%s\n", mark, r.Doc.ID, r.Doc.Version, r.FileName, extra)
		for _, m := range r.Missing {
			values[m.Key] = m.What
		}
	}
	fmt.Fprintf(out, "✓ %d document(s) rendered as drafts to %s; open %s\n", len(rendered), dir, filepath.Join(dir, "index.html"))
	if len(values) > 0 {
		fmt.Fprintf(out, "Values the owner must supply before these documents can be approved (%d):\n", len(values))
		for _, k := range sortedKeys(values) {
			fmt.Fprintf(out, "  - %s: %s\n", k, values[k])
		}
	}
	return nil
}

func splitErrors(err error) []error {
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		return j.Unwrap()
	}
	return []error{err}
}

var renderIndexTemplate = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Policy documents (preview)</title>
<style>body{font-family:system-ui,sans-serif;margin:2rem;color:#222}table{border-collapse:collapse;width:100%}
th,td{border:1px solid #ccc;padding:.4rem .6rem;text-align:left;vertical-align:top}th{background:#f3f3f3}.small{color:#666;font-size:.85em}</style>
</head><body><h1>Policy documents (preview)</h1>
<p class="small">Rendered as drafts. Nothing has been sent to the tenant: the owner approves each document before it is published.</p>
<table><thead><tr><th>Document</th><th>Version</th><th>Kind</th><th>Owner / approver</th><th>Review</th><th>Requirements</th><th>To be completed</th><th>File</th><th>Source</th></tr></thead><tbody>
{{range .}}<tr><td><b>{{.Doc.ID}}</b> {{.Doc.Title}}</td><td>{{.Doc.Version}}</td><td>{{.Doc.Kind}}</td><td>{{.Doc.OwnerRole}} / {{.Doc.ApproverRole}}</td>
<td>every {{.Months}} months</td><td>{{.Reqs}}</td><td class="small">{{range .Missing}}<div>{{.Key}}</div>{{else}}none{{end}}</td><td><a href="{{.Href}}">{{.FileName}}</a></td><td class="small">{{.Short}}</td></tr>
{{end}}</tbody></table></body></html>
`))

func renderIndex(rs []*docs.Rendered) ([]byte, error) {
	type row struct {
		*docs.Rendered
		Months      int
		Reqs, Short string
		Href        template.URL
	}
	var rows []row
	for _, r := range rs {
		m := r.Doc.ReviewMonths
		if m <= 0 {
			m = docs.DefaultReviewMonths
		}
		rows = append(rows, row{Rendered: r, Months: m, Reqs: strings.Join(r.Doc.Requirements, ", "), Short: r.Source[:12],
			Href: template.URL(url.PathEscape(r.FileName))})
	}
	var buf bytes.Buffer
	err := renderIndexTemplate.Execute(&buf, rows)
	return buf.Bytes(), err
}

// ---------- the sharepoint.policies step's input ----------

// policiesInput renders every document for the sharepoint.policies step
// ("@policies" in its reads). Each document carries its draft and, when
// the owner has approved publishing that exact content (SPEC §25.1), the
// approved version with who approved it and when: the step publishes only
// what the owner approved.
func (s *session) policiesInput(ctx context.Context) (map[string]any, error) {
	s.policiesMu.Lock()
	defer s.policiesMu.Unlock()
	if s.policiesIn != nil || s.policiesErr != nil {
		return s.policiesIn, s.policiesErr
	}
	s.policiesIn, s.policiesErr = s.buildPoliciesInput(ctx)
	return s.policiesIn, s.policiesErr
}

func (s *session) buildPoliciesInput(ctx context.Context) (map[string]any, error) {
	dict, source := s.dictionary, "file"
	if t, err := s.tenantDictionary(ctx); err != nil {
		s.warn(PoliciesStep, "Dictionary not read from the tenant, using --dictionary values: "+err.Error())
	} else if t != nil {
		dict, source = docs.Merge(s.dictionary, t), "tenant"
	}
	rc, err := renderContext(s.root, s.doc, dict)
	if err != nil {
		return nil, err
	}
	ds, err := loadDocs(s.root)
	if err != nil {
		return nil, err
	}
	drafts, err := docs.RenderAll(ds, rc)
	if err != nil {
		return nil, err
	}
	approvals := s.approvals.Approvals()
	var list []any
	needed := map[string]string{} // Dictionary keys without a value, for the owner to fill
	for _, r := range drafts {
		target, detail := docs.ApproveChange(r)
		d := map[string]any{"id": r.Doc.ID, "title": r.Doc.Title, "version": r.Doc.Version, "kind": r.Doc.Kind,
			"file": r.FileName, "pdf": r.PDFName, "requirements": strings.Join(r.Doc.Requirements, ", "), "source": r.Source,
			"draft":   map[string]any{"content": base64.StdEncoding.EncodeToString(r.DOCX), "hash": r.Hash},
			"approve": map[string]any{"action": "update", "target": target, "detail": detail}}
		if len(r.Missing) > 0 {
			// Stays a draft: no publish change is planned until every value is supplied.
			var miss []any
			for _, m := range r.Missing {
				miss = append(miss, map[string]any{"key": m.Key, "what": m.What})
				if k, ok := strings.CutPrefix(m.Key, "dictionary."); ok {
					needed[k] = m.What
				}
			}
			d["missing"] = miss
			delete(d, "approve")
		}
		for _, a := range approvals {
			if len(r.Missing) > 0 || a.Step != PoliciesStep || a.AppliedAt != nil || a.Target != target || a.Detail != detail {
				continue
			}
			ap, err := docs.Render(r.Doc, rc, &docs.Approval{By: a.By, On: a.At})
			if err != nil {
				return nil, err
			}
			d["approval"] = map[string]any{"by": ap.ApprovedBy, "on": ap.ApprovedOn.Format(docs.DateISO), "review_due": ap.ReviewDue.Format(docs.DateISO),
				"content": base64.StdEncoding.EncodeToString(ap.DOCX), "hash": ap.Hash, "digest": a.Digest}
			break
		}
		list = append(list, d)
	}
	var dictNeeded []any
	for _, k := range sortedKeys(needed) {
		dictNeeded = append(dictNeeded, map[string]any{"key": k, "label": needed[k]})
	}
	return map[string]any{"library": PoliciesLibrary, "dictionary_source": source, "documents": list, "dictionary_needed": dictNeeded}, nil
}

// warn emits a warning event.
func (s *session) warn(step, msg string) {
	if s.sink != nil {
		s.sink.Emit(events.Event{T: time.Now().UTC(), Ev: events.Log, Step: step, Level: "warning", Msg: msg})
	}
}

// tenantDictionary reads the Dictionary list in the Admin site over Graph
// with the pipeline's token (Sites.FullControl.All in the base bundle; this
// is a read). It returns nil, nil when the tenant is not bootstrapped.
func (s *session) tenantDictionary(ctx context.Context) (map[string]string, error) {
	if s.graphToken == nil || s.pipeline == nil {
		return nil, nil
	}
	name, _, _ := strings.Cut(strings.ToLower(s.pipeline.InitialDomain), ".")
	admin := ""
	for _, st := range s.doc.Config.Structure.Sites {
		if st.Name == "Admin" {
			admin = st.URL
		}
	}
	if name == "" || admin == "" {
		return nil, errors.New("no initial domain or Admin site in the configuration")
	}
	tok, err := s.graphToken(ctx)
	if err != nil {
		return nil, err
	}
	gc := &graph.Client{Token: func() string { return tok }}
	return readTenantDictionary(ctx, gc, graph.Base, name+".sharepoint.com", admin)
}

// readTenantDictionary reads every Dictionary item's key and value.
func readTenantDictionary(ctx context.Context, gc *graph.Client, base, host, adminURL string) (map[string]string, error) {
	var site struct {
		ID string `json:"id"`
	}
	if err := gc.Get(ctx, base+"/sites/"+host+":/sites/"+url.PathEscape(adminURL)+"?$select=id", &site); err != nil {
		return nil, fmt.Errorf("Admin site: %w", err)
	}
	out := map[string]string{}
	next := base + "/sites/" + site.ID + "/lists/Dictionary/items?$expand=fields($select=key,value)&$top=500"
	for next != "" {
		var page struct {
			Value []struct {
				Fields map[string]any `json:"fields"`
			} `json:"value"`
			Next string `json:"@odata.nextLink"`
		}
		if err := gc.Get(ctx, next, &page); err != nil {
			return nil, fmt.Errorf("Dictionary: %w", err)
		}
		for _, it := range page.Value {
			k, _ := it.Fields["key"].(string)
			v, _ := it.Fields["value"].(string)
			if k != "" && strings.TrimSpace(v) != "" {
				out[k] = v
			}
		}
		next = page.Next
	}
	return out, nil
}
