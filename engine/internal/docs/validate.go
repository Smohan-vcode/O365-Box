package docs

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
)

var (
	idPattern = map[string]*regexp.Regexp{
		KindPolicy:    regexp.MustCompile(`^POL-\d{2}$`),
		KindProcedure: regexp.MustCompile(`^PRC-\d{2}$`),
	}
	flowID        = regexp.MustCompile(`^F\d{2,}$`)
	placeholderRe = regexp.MustCompile(`\{\{\s*([^{}]*?)\s*\}\}`)
	keyRe         = regexp.MustCompile(`^(dictionary|generated|jurisdictions)\.[a-z0-9_.-]+$`)
	emailRe       = regexp.MustCompile(`(?i)[a-z0-9._%+-]+@[a-z0-9-]+(\.[a-z0-9-]+)+`)
	domainRe      = regexp.MustCompile(`(?i)\b[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*\.(?:com|net|org|io|co|uk|eu|de|fr|us|ca|au|nz|ie|nl|es|it|ch|sg|in|jp|hk|ae|br|mx|example|info|biz|app|dev|ai|gov|edu|onmicrosoft)\b`)
)

// Validate checks the loaded documents against the requirement ids of
// policy/requirements.yaml and the role names of defaults/roles.yaml. It
// returns one message per problem, "<id>: <problem>".
func Validate(docs []Doc, reqIDs, roles []string) []string {
	reqs, roleSet := set(reqIDs), set(roles)
	var out []string
	seen := map[string]bool{}
	for _, d := range docs {
		add := func(format string, a ...any) { out = append(out, d.ID+": "+fmt.Sprintf(format, a...)) }
		if d.ID == "" {
			add("index entry has no id")
		}
		if seen[d.ID] {
			add("id repeated in the index")
		}
		seen[d.ID] = true
		if re, ok := idPattern[d.Kind]; !ok {
			add("kind %q is not policy or procedure", d.Kind)
		} else if !re.MatchString(d.ID) {
			add("id does not fit kind %s", d.Kind)
		}
		for _, p := range d.problems {
			add("%s", p)
		}
		if !d.hasMatter {
			continue
		}
		for _, k := range d.missingKey {
			add("front matter has no %s", k)
		}
		if d.fmID != d.ID {
			add("front matter id %q differs from the index", d.fmID)
		}
		if d.fmTitle != d.Title {
			add("front matter title %q differs from the index title %q", d.fmTitle, d.Title)
		}
		if strings.TrimSpace(d.Version) == "" {
			add("no version")
		}
		if d.ReviewMonths <= 0 {
			add("review_months must be a positive number of months")
		}
		for _, r := range []struct{ key, v string }{{"owner_role", d.OwnerRole}, {"approver_role", d.ApproverRole}} {
			if !roleSet[r.v] {
				add("%s %q is not a role in defaults/roles.yaml", r.key, r.v)
			}
		}
		for _, r := range d.Requirements {
			if !reqs[r] {
				add("unknown requirement %s", r)
			}
		}
		if !sameSet(d.Requirements, d.indexReqs) {
			add("requirements in the index %v differ from the front matter %v", d.indexReqs, d.Requirements)
		}
		if len(d.EnforcedBy) == 0 {
			add("enforced_by names no requirement id, flow id or manual")
		}
		for _, e := range d.EnforcedBy {
			if e != "manual" && !reqs[e] && !flowID.MatchString(e) {
				add("enforced_by %q is not a requirement id, a flow id or manual", e)
			}
		}
		if d.Kind == KindProcedure && len(d.Records) == 0 {
			add("a procedure names the recurring record that proves it is followed")
		}
		out = append(out, checkBody(d, roleSet)...)
	}
	return out
}

// checkBody checks placeholder syntax, role names inside role placeholders,
// and literal email addresses or domains outside placeholders (R1).
func checkBody(d Doc, roles map[string]bool) []string {
	var out []string
	add := func(line int, format string, a ...any) {
		out = append(out, fmt.Sprintf("%s: line %d: ", d.ID, line)+fmt.Sprintf(format, a...))
	}
	for i, line := range strings.Split(d.Body, "\n") {
		n := i + 1
		for _, m := range placeholderRe.FindAllStringSubmatch(line, -1) {
			key := m[1]
			if !keyRe.MatchString(key) {
				add(n, "placeholder {{%s}} is not dictionary.*, generated.* or jurisdictions.*", key)
				continue
			}
			if r, ok := strings.CutPrefix(key, "dictionary.role."); ok {
				name, _, _ := strings.Cut(r, ".")
				if !roles[name] {
					add(n, "placeholder {{%s}} names unknown role %q", key, name)
				}
			}
		}
		plain := placeholderRe.ReplaceAllString(line, " ")
		if strings.Contains(plain, "{{") || strings.Contains(plain, "}}") {
			add(n, "unbalanced placeholder braces")
		}
		if m := emailRe.FindString(plain); m != "" {
			add(n, "literal email address %q outside a placeholder", m)
		} else if m := domainRe.FindString(plain); m != "" {
			add(n, "literal domain %q outside a placeholder", m)
		}
	}
	return out
}

// CheckPlaceholders reports placeholders used in a body that are not
// documented in PLACEHOLDERS.md.
func CheckPlaceholders(docs []Doc, known map[string]string) []string {
	var out []string
	for _, d := range docs {
		for _, m := range placeholderRe.FindAllStringSubmatch(d.Body, -1) {
			if _, ok := known[m[1]]; !ok && keyRe.MatchString(m[1]) {
				out = append(out, fmt.Sprintf("%s: placeholder {{%s}} is not listed in %s", d.ID, m[1], PlaceholdersFile))
			}
		}
	}
	return uniq(out)
}

// CheckControls reports a framework control in a document's front matter
// that the framework file does not map to one of the document's
// requirements (copied from policy/frameworks, never invented).
func CheckControls(docs []Doc, frameworks []policy.Framework) []string {
	maps := map[string]map[string][]string{}
	for _, fw := range frameworks {
		m := map[string][]string{}
		for _, c := range fw.Controls {
			m[c.ID] = c.Requirements
		}
		maps[fw.ID] = m
	}
	var out []string
	for _, d := range docs {
		reqs := set(d.Requirements)
		for fw, ids := range d.Controls {
			m, ok := maps[fw]
			if !ok {
				out = append(out, fmt.Sprintf("%s: controls name unknown framework %q", d.ID, fw))
				continue
			}
			for _, id := range ids {
				hit := false
				for _, r := range m[id] {
					hit = hit || reqs[r]
				}
				if !hit {
					out = append(out, fmt.Sprintf("%s: %s control %s does not map any of the document's requirements", d.ID, fw, id))
				}
			}
		}
	}
	return out
}

// MustCover returns the requirements a written document must cover: every
// procedural requirement, and every mixed requirement whose evidence is a
// recurring task (task.*).
func MustCover(reqs []policy.Requirement) []string {
	var out []string
	for _, r := range reqs {
		if r.Type == policy.TypeProcedural || (r.Type == policy.TypeMixed && strings.HasPrefix(r.Evidence, "task.")) {
			out = append(out, r.ID)
		}
	}
	return out
}

// Uncovered returns the ids in must that no document lists in its
// requirements.
func Uncovered(docs []Doc, must []string) []string {
	covered := map[string]bool{}
	for _, d := range docs {
		for _, r := range d.Requirements {
			covered[r] = true
		}
	}
	var out []string
	for _, id := range must {
		if !covered[id] {
			out = append(out, id)
		}
	}
	return out
}

func set(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func sameSet(a, b []string) bool {
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	return strings.Join(x, ",") == strings.Join(y, ",")
}

func uniq(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
