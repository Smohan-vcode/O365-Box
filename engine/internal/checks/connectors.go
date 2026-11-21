// Package checks implements the repository checks check-literals and
// check-connectors (SPEC §2 R1 and R2).
package checks

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Smohan-vcode/O365-Box/engine/internal/report"
)

// ConnectorPaths names the files check-connectors reads, relative to root.
type ConnectorPaths struct {
	AllowList string // expected-state/standard-connectors.yaml
	Catalogue string // flows/catalogue.yaml
	Solution  string // flows/solution
}

// DefaultConnectorPaths are the repository's own files.
var DefaultConnectorPaths = ConnectorPaths{
	AllowList: "expected-state/standard-connectors.yaml",
	Catalogue: "flows/catalogue.yaml",
	Solution:  "flows/solution",
}

type allowList struct {
	Allowed map[string]struct {
		Name string   `yaml:"name"`
		API  []string `yaml:"api"`
	} `yaml:"allowed"`
}

type catalogue struct {
	Flows []struct {
		ID         string   `yaml:"id"`
		Name       string   `yaml:"name"`
		Connectors []string `yaml:"connectors"`
	} `yaml:"flows"`
}

var apiRefs = []*regexp.Regexp{
	regexp.MustCompile(`/providers/Microsoft\.PowerApps/apis/([A-Za-z0-9_]+)`),
	regexp.MustCompile(`"apiName"\s*:\s*"([A-Za-z0-9_]+)"`),
	regexp.MustCompile(`"api"\s*:\s*\{\s*"name"\s*:\s*"([A-Za-z0-9_]+)"`),
}

// Connectors checks that every connector used by a flow is on the allow-list.
// It returns the issues and the number of connector uses checked.
func Connectors(root string, p ConnectorPaths) ([]report.Issue, int, error) {
	var al allowList
	if err := readYAML(filepath.Join(root, p.AllowList), &al); err != nil {
		return nil, 0, err
	}
	if len(al.Allowed) == 0 {
		return nil, 0, fmt.Errorf("%s: no allowed connectors", p.AllowList)
	}
	keys := map[string]bool{}
	apis := map[string]bool{}
	for k, v := range al.Allowed {
		keys[k] = true
		for _, a := range v.API {
			apis[strings.ToLower(a)] = true
		}
	}
	var issues []report.Issue
	checked := 0

	var cat catalogue
	if err := readYAML(filepath.Join(root, p.Catalogue), &cat); err != nil {
		return nil, 0, err
	}
	lines := lineIndex(filepath.Join(root, p.Catalogue))
	for _, f := range cat.Flows {
		if len(f.Connectors) == 0 {
			issues = append(issues, report.Issue{Check: "connectors", File: p.Catalogue, Line: lines["{id: "+f.ID+","],
				Message: fmt.Sprintf("flow %s declares no connectors", f.ID)})
		}
		for _, c := range f.Connectors {
			checked++
			if !keys[c] {
				issues = append(issues, report.Issue{Check: "connectors", File: p.Catalogue, Line: lines["{id: "+f.ID+","],
					Message: fmt.Sprintf("flow %s (%s) uses connector %q, which is not in %s", f.ID, f.Name, c, p.AllowList)})
			}
		}
	}

	solDir := filepath.Join(root, p.Solution)
	if st, err := os.Stat(solDir); err == nil && st.IsDir() {
		err := filepath.WalkDir(solDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".json") {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
			n := 0
			for sc.Scan() {
				n++
				for _, re := range apiRefs {
					for _, m := range re.FindAllStringSubmatch(sc.Text(), -1) {
						checked++
						if !apis[strings.ToLower(m[1])] {
							issues = append(issues, report.Issue{Check: "connectors", File: rel, Line: n,
								Message: fmt.Sprintf("connector API %q is not in %s", m[1], p.AllowList)})
						}
					}
				}
			}
			return sc.Err()
		})
		if err != nil {
			return nil, 0, err
		}
	}
	report.Sort(issues)
	return issues, checked, nil
}

func readYAML(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// lineIndex maps each distinctive line prefix "{id: X," to its line number.
func lineIndex(path string) map[string]int {
	out := map[string]int{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for i, l := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "-"))
		if strings.HasPrefix(t, "{id: ") {
			if j := strings.Index(t, ","); j > 0 {
				out[t[:j+1]] = i + 1
			}
		}
	}
	return out
}
