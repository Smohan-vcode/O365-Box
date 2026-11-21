package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const acmeConfig = "../../../examples/acme/box-config.yaml"

func TestPolicyValidate(t *testing.T) {
	code, out := box(t, "policy", "validate")
	if code != ExitOK || !strings.Contains(out, "90 requirements, 333 settings in 12 centres, 5 frameworks") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	code, out = box(t, "policy", "validate", "--json")
	var res struct {
		OK bool `json:"ok"`
	}
	if code != ExitOK || json.Unmarshal([]byte(out), &res) != nil || !res.OK {
		t.Fatalf("--json: exit %d\n%s", code, out)
	}
}

func TestPolicyResolve(t *testing.T) {
	dir := t.TempDir()
	code, out := box(t, "policy", "resolve", "--config", acmeConfig, "--out", dir)
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, want := range []string{"plan business_premium, tier core, stage audit", "applied 303 settings in 12 centres", "not applied 30"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q\n%s", want, out)
		}
	}
	for _, f := range []string{"entra.yaml", "teams.yaml", "not-applied.yaml", "coverage.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Error(err)
		}
	}

	code, out = box(t, "policy", "resolve", "--config", acmeConfig, "--plan", "business_basic", "--json")
	var res struct {
		Plan       string `json:"plan"`
		NotApplied []struct {
			ID, Reason string
		} `json:"not_applied"`
	}
	if code != ExitOK || json.Unmarshal([]byte(out), &res) != nil || res.Plan != "business_basic" {
		t.Fatalf("--json: exit %d\n%s", code, out)
	}
	found := false
	for _, n := range res.NotApplied {
		found = found || (n.ID == "ENT-001" && strings.HasPrefix(n.Reason, "needs "))
	}
	if !found {
		t.Error("business_basic should leave out ENT-001 for a missing capability")
	}

	code, out = box(t, "policy", "resolve", "--config", acmeConfig, "--out", "testdata/should-not-exist")
	if code != ExitValidation || !strings.Contains(out, "inside the repository") {
		t.Errorf("--out inside the repository: exit %d\n%s", code, out)
	}
	if _, err := os.Stat("testdata/should-not-exist"); err == nil {
		t.Error("output written inside the repository")
	}
	code, out = box(t, "policy", "resolve", "--config", acmeConfig, "--plan", "business_ultimate")
	if code != ExitValidation || !strings.Contains(out, "no plan") {
		t.Errorf("unknown plan: exit %d\n%s", code, out)
	}
}

func TestPolicyCoverage(t *testing.T) {
	code, out := box(t, "policy", "coverage", "--config", acmeConfig)
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, want := range []string{"# Coverage report", "## SOC 2 Trust Services Criteria", "## NIST Cybersecurity Framework 2.0", "| TMS-005 |"} {
		if !strings.Contains(out, want) {
			t.Errorf("coverage lacks %q", want)
		}
	}
	if strings.Contains(out, "## CIS Critical Security Controls") {
		t.Error("coverage shows a framework that is not selected")
	}
}

func TestPolicyRefusesInvalidConfig(t *testing.T) {
	b, err := os.ReadFile(acmeConfig)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(b), "ends: '2099-12-31'", "ends: '2020-01-01'", 1)
	text = strings.Replace(text, "  - nist_csf_2\n", "  - made_up_framework\n", 1)
	p := filepath.Join(t.TempDir(), "box-config.yaml")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"policy", "resolve", "--config", p}, {"config", "validate", p}} {
		code, out := box(t, args...)
		if code != ExitValidation {
			t.Fatalf("%v: exit %d\n%s", args, code, out)
		}
		for _, want := range []string{"policy.exceptions[0].ends: exception for TMS-005 expired on 2020-01-01",
			`policy.frameworks[1]: framework "made_up_framework" is not in policy/frameworks`} {
			if !strings.Contains(out, want) {
				t.Errorf("%v: output lacks %q\n%s", args, want, out)
			}
		}
	}
}
