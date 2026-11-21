package checks

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
	"github.com/Smohan-vcode/O365-Box/engine/internal/repo"
	"github.com/Smohan-vcode/O365-Box/engine/internal/report"
)

// Literal is one company value that must not appear in the repository.
type Literal struct {
	Value  string // as written in the configuration
	Source string // where it came from, e.g. company.legal_name
}

const minLiteral = 4

// excludedDictionarySuffixes are dictionary keys whose values are too
// generic to search for (country and state names appear in engine rules).
var excludedDictionarySuffixes = []string{".country", ".state"}

// LiteralsFrom collects the company values of a configuration document and
// its dictionary (SPEC §2 R1): names and entity wording, domains and their
// first label, the tenant ID, people's names, and dictionary values.
func LiteralsFrom(d *config.Document, dictionary map[string]string) []Literal {
	c := d.Config
	var out []Literal
	add := func(v, src string) {
		v = strings.TrimSpace(v)
		if len([]rune(v)) >= minLiteral {
			out = append(out, Literal{Value: v, Source: src})
		}
	}
	add(c.Company.LegalName, "company.legal_name")
	add(c.Company.TradingName, "company.trading_name")
	add(c.Company.EntityWording, "company.entity_wording")
	domains := append([]string{c.Company.PrimaryDomain}, c.Company.DomainAliases...)
	for i, dom := range domains {
		src := "company.primary_domain"
		if i > 0 {
			src = fmt.Sprintf("company.domain_aliases[%d]", i-1)
		}
		add(dom, src)
		if label, _, ok := strings.Cut(dom, "."); ok {
			add(label, src+" (first label)")
		}
	}
	add(c.Tenant.TenantID, "tenant.tenant_id")
	for i, u := range c.Users {
		if u.BreakGlass {
			continue
		}
		add(strings.TrimSpace(u.GivenName+" "+u.FamilyName), fmt.Sprintf("users[%d] name", i))
		if len([]rune(u.FamilyName)) >= 5 {
			add(u.FamilyName, fmt.Sprintf("users[%d].family_name", i))
		}
	}
	keys := make([]string, 0, len(dictionary))
	for k := range dictionary {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		skip := false
		for _, s := range excludedDictionarySuffixes {
			skip = skip || strings.HasSuffix(k, s)
		}
		if !skip {
			add(dictionary[k], "dictionary "+k)
		}
	}
	return dedupeLiterals(out)
}

func dedupeLiterals(in []Literal) []Literal {
	seen := map[string]bool{}
	var out []Literal
	for _, l := range in {
		k := strings.ToLower(l.Value)
		if !seen[k] {
			seen[k] = true
			out = append(out, l)
		}
	}
	return out
}

type secretPattern struct {
	name string
	re   *regexp.Regexp
}

var secretPatterns = []secretPattern{
	{"private key", regexp.MustCompile(`-----BEGIN ([A-Z]+ )*PRIVATE KEY-----`)},
	{"JSON web token", regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
	{"GitHub token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)},
	{"AWS access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{"assigned secret", regexp.MustCompile(`(?i)\b(client_secret|clientsecret|api_key|apikey|password|secret)\s*[:=]\s*["']?[A-Za-z0-9/+_~.\-]{16,}`)},
}

// ScanOptions controls what Literals scans.
type ScanOptions struct {
	Skip func(rel string) bool // paths (relative, slash-separated) to leave out
}

// Literals scans every text file under root for the given company values
// and for anything that looks like a secret. It returns the issues and the
// number of files scanned.
func Literals(root string, lits []Literal, opt ScanOptions) ([]report.Issue, int, error) {
	files, err := repo.TextFiles(root, opt.Skip)
	if err != nil {
		return nil, 0, err
	}
	type needle struct {
		lit   Literal
		lower string
		word  bool
	}
	needles := make([]needle, 0, len(lits))
	for _, l := range lits {
		needles = append(needles, needle{lit: l, lower: strings.ToLower(l.Value), word: len([]rune(l.Value)) < 6})
	}
	var issues []report.Issue
	for _, rel := range files {
		f, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, 0, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
		n := 0
		for sc.Scan() {
			n++
			line := sc.Text()
			lower := strings.ToLower(line)
			for _, nd := range needles {
				if containsLiteral(lower, nd.lower, nd.word) {
					issues = append(issues, report.Issue{Check: "literals", File: rel, Line: n,
						Message: fmt.Sprintf("company value from %s: %q", nd.lit.Source, nd.lit.Value)})
				}
			}
			for _, sp := range secretPatterns {
				if sp.re.MatchString(line) {
					issues = append(issues, report.Issue{Check: "literals", File: rel, Line: n,
						Message: "looks like a secret (" + sp.name + ")"})
				}
			}
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, 0, fmt.Errorf("%s: %w", rel, err)
		}
	}
	report.Sort(issues)
	return issues, len(files), nil
}

func containsLiteral(haystack, needle string, word bool) bool {
	for start := 0; ; {
		i := strings.Index(haystack[start:], needle)
		if i < 0 {
			return false
		}
		i += start
		if !word || (boundary(haystack, i-1) && boundary(haystack, i+len(needle))) {
			return true
		}
		start = i + 1
	}
}

func boundary(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	r := rune(s[i])
	return !(unicode.IsLetter(r) || unicode.IsDigit(r))
}
