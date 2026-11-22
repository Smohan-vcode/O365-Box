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
		steps: func(_ context.Context, _ string, _ *auditFlags, _ io.Writer, by map[string][]policy.StepSetting, tenant map[string]any, creds auditCreds) map[string]audit.StepOutcome {
			*gotToken = creds.graph
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

// T16-06: a reader-only sign-in gets read-only tokens for Exchange Online,
// Security & Compliance, SharePoint and Teams (one more device-code sign-in
// per Microsoft client, as the same account); the provider workers get them
// under the names they read; nothing is printed or kept.
func TestAuditWorkloadTokens(t *testing.T) {
	signedIn, token := false, ""
	env := fakeAuditEnv(t, &signedIn, &token)
	claims := map[string]any{"tid": audittest.Tenant, "oid": "00000000-0000-4000-8000-0000000000bb", "upn": "reader@fernhill.example",
		"scp": "User.Read.All Policy.Read.All openid", "wids": []any{audit.RoleGlobalReader}}
	env.signIn = func(context.Context, string, []string, io.Writer) (*auth.Token, error) {
		return &auth.Token{Access: audittest.Token(claims), Refresh: "refresh-graph-0123456789", Claims: claims, ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	var signIns, redeems []string
	issue := func(client string, scopes []string) *auth.Token {
		return &auth.Token{Access: "workload-token-" + client + "-" + scopes[0], Refresh: "refresh-" + client + "-0123456789", Claims: claims,
			ExpiresAt: time.Date(2026, 11, 21, 13, 0, 0, 0, time.UTC)}
	}
	env.signInClient = func(_ context.Context, tenant, client string, scopes []string, title string, _ io.Writer) (*auth.Token, error) {
		if tenant != audittest.Tenant {
			t.Errorf("sign-in tenant %s", tenant)
		}
		signIns = append(signIns, title)
		return issue(client, scopes), nil
	}
	env.redeem = func(_ context.Context, _, client, refresh string, scopes []string) (*auth.Token, error) {
		redeems = append(redeems, scopes[0])
		return issue(client, scopes), nil
	}
	var creds map[string]map[string]any
	env.steps = func(ctx context.Context, _ string, _ *auditFlags, _ io.Writer, by map[string][]policy.StepSetting, tenant map[string]any, c auditCreds) map[string]audit.StepOutcome {
		if tenant["sharepoint_admin"] != "https://fernhilllabs-admin.sharepoint.com" {
			t.Errorf("tenant = %v", tenant)
		}
		creds = map[string]map[string]any{}
		for _, p := range []string{"identity", "exchange", "sharepoint", "teams"} {
			m, err := auditWorkerCreds(ctx, p, c, func(string) {})
			if err != nil {
				t.Fatal(err)
			}
			creds[p] = m
		}
		drift, _ := json.Marshal(map[string]any{"ok": false,
			"drift":   []map[string]string{{"action": "update", "target": "tenant SharingCapability", "detail": "ExternalUserSharingOnly -> ExistingExternalUserSharingOnly (SPO-001)"}},
			"skipped": []map[string]string{{"id": "SPO-018", "reason": "role assignments: reads each site's content, which a Global Reader cannot open"}}})
		return map[string]audit.StepOutcome{"policy.sharepoint": {Step: "policy.sharepoint", Provider: "sharepoint", Ran: true, Verify: drift}}
	}
	var buf bytes.Buffer
	out := filepath.Join(t.TempDir(), "pack")
	f := &auditFlags{framework: "soc2", period: "2026-01-01..2026-11-21", out: out, tenant: "organizations", cap: 100}
	if err := runAudit(context.Background(), &globals{repo: "../../.."}, f, env, &buf); err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	text := buf.String()
	// Teams needs its Graph token from its own client: redeemed from the
	// Teams sign-in, so no extra code is asked for.
	if strings.Join(signIns, "|") != "Exchange Online and Security & Compliance (Purview)|SharePoint admin|Microsoft Teams and Microsoft Graph for Teams" || len(redeems) != 2 ||
		redeems[0] != "https://ps.compliance.protection.outlook.com/.default" || redeems[1] != "https://graph.microsoft.com/.default" {
		t.Errorf("sign-ins %q, redeems %q", signIns, redeems)
	}
	for _, want := range []string{"✓ Exchange Online: signed in read-only", "✓ Microsoft Teams: signed in read-only", "not built yet"} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "workload-token") || strings.Contains(text, "refresh-") {
		t.Errorf("a token was printed\n%s", text)
	}
	get := func(p, k string) string {
		m, _ := creds[p][k].(map[string]any)
		s, _ := m["access_token"].(string)
		return s
	}
	graphTok := get("identity", "graph")
	if graphTok == "" || get("identity", "app") != graphTok || get("exchange", "graph") != graphTok ||
		!strings.Contains(get("exchange", "app"), "outlook.office365.com") || !strings.Contains(get("exchange", "ipps"), "ps.compliance") ||
		!strings.Contains(get("sharepoint", "app"), "-admin.sharepoint.com") || !strings.Contains(get("teams", "teams"), "48ac35b8") || get("teams", "app") != "" {
		t.Errorf("worker credentials = %v", creds)
	}
	b, _ := os.ReadFile(filepath.Join(out, "evidence", "settings", "all-settings.json"))
	if strings.Contains(string(b), "workload-token") || strings.Contains(string(b), "refresh-") {
		t.Error("a token was written to the pack")
	}
	got := settingReads(t, b)
	if got["SPO-001"].Status != audit.Fail || got["SPO-018"].Status != audit.NotRead || got["SPO-003"].Status != audit.Pass {
		t.Errorf("sharepoint reads: %+v %+v %+v", got["SPO-001"], got["SPO-018"], got["SPO-003"])
	}
	if s := got["EXO-001"]; s.Status != audit.NotRead || !strings.Contains(s.Detail, "policy.exchange is not built yet") {
		t.Errorf("EXO-001 = %+v", s)
	}
}

func settingReads(t *testing.T, b []byte) map[string]audit.SettingRead {
	t.Helper()
	got := map[string]audit.SettingRead{}
	var list []audit.SettingRead
	if err := json.Unmarshal(b, &list); err == nil {
		for _, s := range list {
			got[s.ID] = s
		}
		return got
	}
	var wrapped struct {
		Settings []audit.SettingRead `json:"settings"`
	}
	if err := json.Unmarshal(b, &wrapped); err != nil {
		t.Fatalf("all-settings.json: %v", err)
	}
	for _, s := range wrapped.Settings {
		got[s.ID] = s
	}
	return got
}

// --graph-only, or a sign-in whose roles could write, asks for no workload
// token.
func TestAuditNoWorkloadTokens(t *testing.T) {
	for name, tc := range map[string]struct {
		wids    []any
		flags   auditFlags
		explain string
	}{
		"graph-only": {[]any{audit.RoleGlobalReader}, auditFlags{graphOnly: true}, "--graph-only"},
		"writer":     {[]any{audit.RoleGlobalAdmin}, auditFlags{partial: true}, "can change the tenant"},
	} {
		signedIn, token := false, ""
		env := fakeAuditEnv(t, &signedIn, &token)
		claims := map[string]any{"tid": audittest.Tenant, "oid": "o", "upn": "x@fernhill.example", "scp": "User.Read.All", "wids": tc.wids}
		env.signIn = func(context.Context, string, []string, io.Writer) (*auth.Token, error) {
			return &auth.Token{Access: audittest.Token(claims), Refresh: "refresh-graph-0123456789", Claims: claims}, nil
		}
		env.signInClient = func(context.Context, string, string, []string, string, io.Writer) (*auth.Token, error) {
			t.Errorf("%s: workload sign-in", name)
			return nil, io.EOF
		}
		env.redeem = func(context.Context, string, string, string, []string) (*auth.Token, error) {
			t.Errorf("%s: refresh token redeemed", name)
			return nil, io.EOF
		}
		f := tc.flags
		f.framework, f.out, f.cap = "soc2", filepath.Join(t.TempDir(), "pack"), 10
		var buf bytes.Buffer
		if err := runAudit(context.Background(), &globals{repo: "../../.."}, &f, env, &buf); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, buf.String())
		}
		b, _ := os.ReadFile(filepath.Join(f.out, "evidence", "settings", "all-settings.json"))
		if !strings.Contains(buf.String()+string(b), tc.explain) {
			t.Errorf("%s: reason %q not reported\n%s", name, tc.explain, buf.String())
		}
	}
}
