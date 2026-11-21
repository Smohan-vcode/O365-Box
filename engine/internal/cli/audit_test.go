package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/audit"
	"github.com/Smohan-vcode/O365-Box/engine/internal/audit/audittest"
	"github.com/Smohan-vcode/O365-Box/engine/internal/auth"
	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
)

// fakeAuditEnv answers the sign-in with a Global Reader token and Graph
// with the recorded fictional tenant. steps records what it was given.
func fakeAuditEnv(t *testing.T, signedIn *bool, gotToken *string) *auditEnv {
	fg := audittest.NewGraph(t)
	return &auditEnv{
		signIn: func(_ context.Context, tenant string, scopes []string, _ io.Writer) (*auth.Token, error) {
			*signedIn = true
			if err := audit.CheckScopes(scopes); err != nil {
				t.Errorf("sign-in asked for %v", err)
			}
			claims := map[string]any{"tid": audittest.Tenant, "upn": "reader@fernhill.example", "scp": "User.Read.All Policy.Read.All openid",
				"wids": []any{audit.RoleGlobalReader}}
			return &auth.Token{Access: audittest.Token(claims), Claims: claims, ExpiresAt: time.Now().Add(time.Hour)}, nil
		},
		http: fg.Server.Client(), v1: fg.V1(), beta: fg.Beta(),
		dns: audittest.Resolver{"fernhill.example": {"v=spf1 -all"}, "_dmarc.fernhill.example": {"v=DMARC1; p=reject"}},
		steps: func(_ context.Context, _ string, _ *auditFlags, _ io.Writer, by map[string][]policy.StepSetting, tenant map[string]any, token string) map[string]audit.StepOutcome {
			*gotToken = token
			if tenant["initial_domain"] != "fernhilllabs.onmicrosoft.com" {
				t.Errorf("tenant input = %v", tenant)
			}
			b, _ := json.Marshal(map[string]any{"ok": true})
			return map[string]audit.StepOutcome{"policy.entra": {Step: "policy.entra", Provider: "identity", Ran: true, Verify: b}}
		},
		now: func() time.Time { return time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC) },
	}
}

func TestAuditCommandWritesPack(t *testing.T) {
	signedIn, token := false, ""
	env := fakeAuditEnv(t, &signedIn, &token)
	out := filepath.Join(t.TempDir(), "pack")
	var buf bytes.Buffer
	g := &globals{repo: "../../.."}
	f := &auditFlags{framework: "soc2", period: "2026-01-01..2026-11-21", out: out, tenant: "organizations", cap: 100}
	if err := runAudit(context.Background(), g, f, env, &buf); err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	if !signedIn || token == "" {
		t.Fatal("no sign-in or no token for the steps")
	}
	text := buf.String()
	for _, want := range []string{"signed in (read only)", "audit pack for SOC 2", "token dropped", "exchange setting(s) not read"} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q\n%s", want, text)
		}
	}
	if problems, err := audit.VerifyManifest(out); err != nil || len(problems) > 0 {
		t.Errorf("manifest: %v %v", problems, err)
	}
	if strings.Contains(text, token) {
		t.Error("the token was printed")
	}
	if dir := os.Getenv("BOX_AUDIT_KEEP"); dir != "" {
		_ = os.CopyFS(dir, os.DirFS(out))
	}
	// The pack can be verified from the command line.
	if code, o := box(t, "audit", "verify", out); code != ExitOK || !strings.Contains(o, "every file matches") {
		t.Errorf("verify: exit %d\n%s", code, o)
	}
	_ = os.WriteFile(filepath.Join(out, "gaps.md"), []byte("changed"), 0o600)
	if code, o := box(t, "audit", "verify", out); code != ExitFailure || !strings.Contains(o, "gaps.md: changed") {
		t.Errorf("verify after change: exit %d\n%s", code, o)
	}
}

// Nothing is read before the folder is accepted: a folder in the
// repository is refused before any sign-in.
func TestAuditRefusesFolderInRepository(t *testing.T) {
	signedIn, token := false, ""
	env := fakeAuditEnv(t, &signedIn, &token)
	g := &globals{repo: "../../.."}
	for _, out := range []string{"../../../audit-pack", "testdata/pack", ""} {
		err := runAudit(context.Background(), g, &auditFlags{framework: "soc2", out: out}, env, io.Discard)
		if _, ok := err.(usageErr); !ok {
			t.Errorf("--out %q: err = %v, want a usage error", out, err)
		}
	}
	if err := runAudit(context.Background(), g, &auditFlags{framework: "nope", out: filepath.Join(t.TempDir(), "p")}, env, io.Discard); err == nil || !strings.Contains(err.Error(), "soc2") {
		t.Errorf("unknown framework: %v", err)
	}
	if signedIn {
		t.Error("signed in although the command was refused")
	}
}

// With a token that could write, held by a person who could write, the
// policy steps get nothing and their settings are reported not read.
func TestAuditWithWriterTokenSkipsProviders(t *testing.T) {
	signedIn, token := false, ""
	env := fakeAuditEnv(t, &signedIn, &token)
	env.signIn = func(context.Context, string, []string, io.Writer) (*auth.Token, error) {
		claims := map[string]any{"tid": audittest.Tenant, "upn": "admin@fernhill.example", "scp": "User.Read.All User.ReadWrite.All",
			"wids": []any{audit.RoleGlobalAdmin}}
		return &auth.Token{Access: audittest.Token(claims), Claims: claims}, nil
	}
	var buf bytes.Buffer
	out := filepath.Join(t.TempDir(), "pack")
	// Without --partial an administrator's sign-in stops before reading.
	if err := runAudit(context.Background(), &globals{repo: "../../.."}, &auditFlags{framework: "soc2", out: out, cap: 10}, env, &buf); err == nil ||
		!strings.Contains(buf.String(), "as admin@fernhill.example; roles: Global Administrator") || !strings.Contains(buf.String(), "private browser window") {
		t.Fatalf("admin sign-in did not stop: %v\n%s", err, buf.String())
	}
	if _, err := os.Stat(filepath.Join(out, "index.html")); err == nil {
		t.Fatal("a pack was written after stopping")
	}
	buf.Reset()
	if err := runAudit(context.Background(), &globals{repo: "../../.."}, &auditFlags{framework: "soc2", out: out, cap: 10, partial: true}, env, &buf); err != nil {
		t.Fatal(err)
	}
	if token != "" {
		t.Error("the policy steps were given a token that can write")
	}
	b, _ := os.ReadFile(filepath.Join(out, "evidence", "settings", "all-settings.json"))
	if !strings.Contains(string(b), "sign in as Global Reader") || !strings.Contains(buf.String(), "User.ReadWrite.All") {
		t.Errorf("reason not reported\n%s", buf.String())
	}
}

func TestAuditHelp(t *testing.T) {
	code, out := box(t, "audit", "--help")
	for _, want := range []string{"--framework", "--period", "--out", "Global Reader", "read-only", "manifest.sha256", "plan and apply are refused"} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q", want)
		}
	}
	if code != ExitOK {
		t.Errorf("exit %d", code)
	}
}
