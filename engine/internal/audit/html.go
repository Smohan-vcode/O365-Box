package audit

import (
	"bytes"
	"html/template"
	"sort"
	"strings"
	"time"
)

// index.html is self-contained: inline style, no script, no external
// asset; links point only at files inside the pack.
var indexTemplate = template.Must(template.New("index").Funcs(template.FuncMap{
	"cls":  func(s string) string { return strings.ReplaceAll(s, " ", "-") },
	"join": strings.Join,
	"time": func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") },
}).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Audit pack: {{.P.Framework.Title}}</title>
<style>
:root{--bg:#fff;--fg:#1d2329;--muted:#5b6670;--line:#d9dee3;--met:#1f7a3d;--partly:#9a6700;--not:#b42318;--doc:#3e4c9a;--nr:#5b6670}
@media (prefers-color-scheme:dark){:root{--bg:#14181c;--fg:#e6e9ec;--muted:#9aa5af;--line:#2e353c;--met:#5cc982;--partly:#e0b040;--not:#f07167;--doc:#9aa8ff;--nr:#9aa5af}}
body{background:var(--bg);color:var(--fg);font:15px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif;margin:0;padding:24px 16px}
main{max-width:1100px;margin:0 auto}
h1{font-size:1.5rem;margin:0 0 4px}h2{font-size:1.15rem;margin:32px 0 8px}
.meta{color:var(--muted);margin:0 0 16px}
table{border-collapse:collapse;width:100%;font-size:.9rem}
th,td{text-align:left;vertical-align:top;padding:6px 8px;border-bottom:1px solid var(--line)}
th{font-weight:600}
.s{font-weight:600;white-space:nowrap}
.met{color:var(--met)}.partly-met{color:var(--partly)}.not-met{color:var(--not)}.document-needed{color:var(--doc)}.not-read,.partly-read{color:var(--nr)}
.read{color:var(--met)}.pass{color:var(--met)}.fail{color:var(--not)}
.tiles{display:flex;flex-wrap:wrap;gap:12px;margin:8px 0}
.tile{border:1px solid var(--line);border-radius:6px;padding:8px 14px;min-width:110px}
.tile b{display:block;font-size:1.4rem}
.small{font-size:.82rem;color:var(--muted)}
.wrap{overflow-x:auto}
a{color:inherit}
ul{margin:4px 0;padding-left:20px}
</style>
</head>
<body><main>
<h1>Audit pack: {{.P.Framework.Title}}</h1>
<p class="meta">Tenant {{.P.Tenant}}{{if .P.Organisation}} ({{.P.Organisation}}){{end}} · period {{.P.Period}} · read {{time .P.ReadAt}}{{if .P.Subject}} by {{.P.Subject}}{{end}} · {{.P.ReadBy}}</p>
<p>This pack was produced by reading the tenant only: nothing was changed. Every evidence file is stamped with the tenant, the time and the query used, and <code>manifest.sha256</code> lists the hash of every file (check with <code>sha256sum -c manifest.sha256</code> in this folder, or <code>box audit verify &lt;folder&gt;</code>).</p>
{{if .P.Framework.Note}}<p class="small">{{.P.Framework.Note}} Framework references are a working mapping, to be validated against the source text.</p>{{end}}
{{if .P.Notes}}<h2>Notes</h2><ul>{{range .P.Notes}}<li>{{.}}</li>{{end}}</ul>{{end}}

<h2>Controls</h2>
<div class="tiles">{{range .Counts}}<div class="tile"><b class="{{cls .Status}}">{{.N}}</b>{{.Status}}</div>{{end}}</div>
<div class="wrap"><table>
<thead><tr><th>Control</th><th>Status</th><th>Requirements</th><th>Gap</th><th>Evidence</th></tr></thead>
<tbody>{{range .P.Assessment.Controls}}
<tr id="{{.ID}}"><td><b>{{.ID}}</b>{{if .Title}}<br><span class="small">{{.Title}}</span>{{end}}</td>
<td class="s {{cls .Status}}">{{.Status}}</td>
<td>{{range .Requirements}}<div><span class="{{cls .Status}}">{{.ID}}</span> <span class="small">{{.Statement}}{{if .Document}} ({{.Document}}{{if .Documents}}: {{.Documents}}{{end}}){{end}}</span></div>{{end}}{{if .Documents}}<div class="small">documents needed: {{join .Documents ", "}}</div>{{end}}</td>
<td class="small">{{.Gap}}</td>
<td class="small">{{range .Evidence}}<div><a href="{{.}}">{{.}}</a></div>{{end}}</td></tr>{{end}}
</tbody></table></div>

<h2>Largest gaps</h2>
<p class="small">All gaps, ordered by risk, with the proposal that would fix each: <a href="gaps.md">gaps.md</a>.</p>
<ol>{{range .TopGaps}}<li><b>{{.Requirement.ID}}</b> <span class="{{cls .Requirement.Status}}">{{.Requirement.Status}}</span>: {{.Requirement.Statement}}<br><span class="small">{{.Requirement.Gap}}</span></li>{{end}}</ol>

<h2>What was read</h2>
<div class="wrap"><table>
<thead><tr><th>Inventory</th><th>Status</th><th>Summary</th><th>Reason / what reading it needs</th></tr></thead>
<tbody>{{range .P.Inventories}}<tr><td><b>{{.ID}}</b><br><span class="small">{{.Title}}</span></td><td class="s {{cls .Status}}">{{.Status}}</td><td>{{.Summary}}{{range .Checks}}{{if not .OK}}<div class="small fail">{{.Severity}}: {{.Text}}</div>{{end}}{{end}}</td><td class="small">{{.Reason}}</td></tr>{{end}}</tbody>
</table></div>

<h2>Catalogue settings</h2>
<p class="small">Each catalogue setting read through its mapping's read and check, with the same verify code the build uses. All settings with their raw state: <a href="evidence/settings/all-settings.json">evidence/settings/all-settings.json</a>.</p>
<div class="wrap"><table>
<thead><tr><th>Area</th><th class="pass">pass</th><th class="fail">fail</th><th class="not-read">not read</th><th>Why not read</th></tr></thead>
<tbody>{{range .Centres}}<tr><td>{{.ID}}</td><td>{{.Pass}}</td><td>{{.Fail}}</td><td>{{.NotRead}}</td><td class="small">{{.Why}}</td></tr>{{end}}</tbody>
</table></div>
<p class="small">Status meanings: <b>met</b> every requirement's settings and checks hold; <b>partly met</b> some hold, or the rest could not be read; <b>not met</b> none hold; <b>document needed</b> the control rests on a written, approved procedure; <b>not read</b> no evidence could be read with this sign-in. Setting reads are the catalogue's exact checks; conclusions drawn from inventories (for example "MFA is required for all users") are indicators, listed in each control's evidence.</p>
</main></body></html>
`))

type centreRow struct {
	ID                  string
	Pass, Fail, NotRead int
	Why                 string
}

type countRow struct {
	Status string
	N      int
}

func (p *Pack) indexHTML() ([]byte, error) {
	var counts []countRow
	for _, s := range []string{Met, PartlyMet, NotMet, DocumentNeeded, NotRead} {
		counts = append(counts, countRow{s, p.Assessment.Counts[s]})
	}
	by := map[string]*centreRow{}
	why := map[string]map[string]bool{}
	for _, s := range p.Settings {
		r := by[s.Centre]
		if r == nil {
			r = &centreRow{ID: s.Centre}
			by[s.Centre] = r
			why[s.Centre] = map[string]bool{}
		}
		switch s.Status {
		case Pass:
			r.Pass++
		case Fail:
			r.Fail++
		default:
			r.NotRead++
			d := s.Detail
			if i := strings.Index(d, ":"); i > 0 && strings.HasPrefix(d, "a person confirms") {
				d = d[:i]
			}
			if len(d) > 160 {
				d = d[:160] + "…"
			}
			why[s.Centre][d] = true
		}
	}
	var centres []centreRow
	for id, r := range by {
		var w []string
		for d := range why[id] {
			w = append(w, d)
		}
		sort.Strings(w)
		if len(w) > 3 {
			w = append(w[:3], "…")
		}
		r.Why = strings.Join(w, " · ")
		centres = append(centres, *r)
	}
	sort.Slice(centres, func(i, j int) bool { return centres[i].ID < centres[j].ID })
	gaps := p.Assessment.Gaps()
	if len(gaps) > 10 {
		gaps = gaps[:10]
	}
	var buf bytes.Buffer
	err := indexTemplate.Execute(&buf, map[string]any{"P": p, "Counts": counts, "Centres": centres, "TopGaps": gaps})
	return buf.Bytes(), err
}
