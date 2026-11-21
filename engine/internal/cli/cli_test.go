package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Smohan-vcode/O365-Box/engine/internal/report"
)

func box(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := RunIn(args, strings.NewReader(""), &out, &errOut)
	return code, out.String() + errOut.String()
}

// boxIn runs box with in as what the user types.
func boxIn(t *testing.T, in string, args ...string) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := RunIn(args, strings.NewReader(in), &out, &errOut)
	return code, out.String() + errOut.String()
}

func TestRepositoryPassesEveryCheck(t *testing.T) {
	for _, args := range [][]string{
		{"config", "validate", "../../../examples/acme/box-config.yaml"},
		{"config", "validate", "../../../examples/acme/box-config.yaml", "--for", "T1-02"},
		{"check-literals"},
		{"check-connectors"},
	} {
		if code, out := box(t, args...); code != ExitOK {
			t.Errorf("box %s: exit %d\n%s", strings.Join(args, " "), code, out)
		}
	}
}

func TestConfigValidateFixtureFails(t *testing.T) {
	code, out := box(t, "config", "validate", "testdata/invalid-config.yaml")
	if code != ExitValidation {
		t.Fatalf("exit %d, want %d\n%s", code, ExitValidation, out)
	}
	for _, want := range []string{
		"tenant.environment: value must be one of",
		"tenant.break_glass_accounts: minItems",
		`tenant.flow_owner: flow owner "nobody" is not a user`,
		`structure.sites[0].access.someone: access is granted to groups only`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q\n%s", want, out)
		}
	}
}

func TestConfigValidateForTask(t *testing.T) {
	code, out := box(t, "config", "validate", "testdata/invalid-config.yaml", "--for", "T0-04", "--json")
	if code != ExitValidation {
		t.Fatalf("exit %d\n%s", code, out)
	}
	var res report.Result
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if res.OK || len(res.Issues) == 0 {
		t.Fatal("want issues")
	}
	code, out = box(t, "config", "validate", "testdata/invalid-config.yaml", "--for", "T99-99")
	if code != ExitFailure || !strings.Contains(out, "no task or spike") {
		t.Errorf("unknown task: exit %d\n%s", code, out)
	}
}

func TestCheckLiteralsFixtureFails(t *testing.T) {
	code, out := box(t, "check-literals", "--dir", "../checks/testdata/literals/repo",
		"--from-file", "../checks/testdata/literals/company.yaml")
	if code != ExitFailure {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, want := range []string{"leak.md:1", "company.legal_name", "secret.txt:2", "private key"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q\n%s", want, out)
		}
	}
	code, out = box(t, "check-literals", "--from-tenant")
	if code != ExitFailure || !strings.Contains(out, "T0-06") {
		t.Errorf("--from-tenant: exit %d\n%s", code, out)
	}
}

func TestCheckConnectorsFixtureFails(t *testing.T) {
	code, out := box(t, "check-connectors",
		"--allow-list", "engine/internal/checks/testdata/connectors/allow.yaml",
		"--catalogue", "engine/internal/checks/testdata/connectors/catalogue.yaml",
		"--solution", "engine/internal/checks/testdata/connectors/solution")
	if code != ExitFailure {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, want := range []string{`uses connector "http"`, `connector API "shared_sendgrid"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q\n%s", want, out)
		}
	}
}
