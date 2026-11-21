package audit

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/auth"
)

const (
	testTenant = "00000000-0000-4000-8000-0000000000aa"
	readerOID  = "00000000-0000-4000-8000-0000000000bb"
	mainClient = "11111111-1111-4111-8111-111111111111"
	exoClient  = "22222222-2222-4222-8222-222222222222"
	tmsClient  = "33333333-3333-4333-8333-333333333333"
	roleSecRdr = "5d6b6bb7-de71-4623-b4af-96380a352509"
)

var testResources = []Resource{
	{Name: "exchange", Title: "Exchange Online", Resource: "https://outlook.office365.com", Client: exoClient, Provider: "exchange", Credential: "app"},
	{Name: "compliance", Title: "Security & Compliance", Resource: "https://ps.compliance.protection.outlook.com", Client: exoClient, Provider: "exchange", Credential: "ipps"},
	{Name: "sharepoint", Title: "SharePoint admin", Resource: "{sharepoint_admin}", Provider: "sharepoint", Credential: "app"},
	{Name: "teams", Title: "Microsoft Teams", Resource: "48ac35b8-9aa8-4d74-927d-1f4a14a0b239", Client: tmsClient, Provider: "teams", Credential: "teams"},
}

func readerClaims(roles ...string) map[string]any {
	w := []any{}
	for _, r := range roles {
		w = append(w, r)
	}
	return map[string]any{"tid": testTenant, "oid": readerOID, "upn": "reader@fernhill.example", "wids": w}
}

func readerSession() Session {
	c := readerClaims(RoleGlobalReader, roleSecRdr)
	return NewSession(testTenant, "reader@fernhill.example", []string{"User.Read.All"}, c)
}

// fakeEntra issues tokens: each sign-in and redemption is recorded, the
// access token names the client and scope, and the refresh token rotates.
type fakeEntra struct {
	signIns, redeems []string
	claims           map[string]any // claims of tokens it issues
	n                int
	exp              time.Time
}

func (f *fakeEntra) source() TokenSource {
	issue := func(client string, scopes []string) *auth.Token {
		f.n++
		return &auth.Token{Access: "access|" + client + "|" + scopes[0], Refresh: "refresh-" + client + "-" + string(rune('a'+f.n)), Claims: f.claims, ExpiresAt: f.exp}
	}
	return TokenSource{
		Redeem: func(_ context.Context, client, refresh string, scopes []string) (*auth.Token, error) {
			f.redeems = append(f.redeems, client+" "+refresh+" "+strings.Join(scopes, " "))
			return issue(client, scopes), nil
		},
		SignIn: func(_ context.Context, client string, scopes []string, title string) (*auth.Token, error) {
			f.signIns = append(f.signIns, client+" "+strings.Join(scopes, " ")+" for "+title)
			return issue(client, scopes), nil
		},
	}
}

func TestTokensForReader(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	fe := &fakeEntra{claims: readerClaims(RoleGlobalReader), exp: now.Add(time.Hour)}
	main := &auth.Token{Access: "graph", Refresh: "refresh-main", Claims: readerClaims(RoleGlobalReader)}
	toks := NewTokens(readerSession(), mainClient, main, fe.source())
	toks.Now = func() time.Time { return now }
	var seen []string
	toks.Acquire(context.Background(), testResources, "https://fernhilllabs-admin.sharepoint.com", func(r Resource, err error) {
		if err != nil {
			t.Errorf("%s: %v", r.Name, err)
		}
		seen = append(seen, r.Name)
	})
	if len(seen) != 4 {
		t.Fatalf("progress = %v", seen)
	}
	// One sign-in per client other than the first: Exchange and
	// Security & Compliance share one; SharePoint uses the Graph sign-in's
	// refresh token.
	want := []string{
		exoClient + " https://outlook.office365.com/.default offline_access for Exchange Online and Security & Compliance",
		tmsClient + " 48ac35b8-9aa8-4d74-927d-1f4a14a0b239/.default offline_access for Microsoft Teams",
	}
	if strings.Join(fe.signIns, "\n") != strings.Join(want, "\n") {
		t.Errorf("sign-ins:\n%s", strings.Join(fe.signIns, "\n"))
	}
	if len(fe.redeems) != 2 || !strings.HasPrefix(fe.redeems[0], exoClient+" refresh-"+exoClient) || !strings.Contains(fe.redeems[0], "ps.compliance.protection.outlook.com/.default") ||
		fe.redeems[1] != mainClient+" refresh-main https://fernhilllabs-admin.sharepoint.com/.default offline_access" {
		t.Errorf("redeems: %q", fe.redeems)
	}
	if got := toks.ForProvider("exchange"); got["app"] != "exchange" || got["ipps"] != "compliance" || len(got) != 2 {
		t.Errorf("exchange creds = %v", got)
	}
	a, err := toks.Token(context.Background(), "teams")
	if err != nil || a != "access|"+tmsClient+"|48ac35b8-9aa8-4d74-927d-1f4a14a0b239/.default" {
		t.Errorf("teams token = %q %v", a, err)
	}
	// Near expiry the token is renewed from that client's refresh token.
	now = now.Add(58 * time.Minute)
	n := len(fe.redeems)
	if a2, err := toks.Token(context.Background(), "teams"); err != nil || len(fe.redeems) != n+1 || !strings.HasPrefix(fe.redeems[n], tmsClient+" refresh-"+tmsClient) || a2 == "" {
		t.Errorf("renewal: %v %q", err, fe.redeems)
	}
	secrets := strings.Join(toks.Secrets(), " ")
	// The rotated refresh tokens replace the ones redeemed.
	if strings.Contains(secrets, "refresh-main") || !strings.Contains(secrets, "refresh-"+mainClient) || !strings.Contains(secrets, "access|"+exoClient) {
		t.Errorf("secrets = %s", secrets)
	}
	toks.Drop()
	if toks.Has("teams") || len(toks.Secrets()) != 0 || main.Refresh != "refresh-main" {
		t.Errorf("after Drop: has=%v secrets=%v", toks.Has("teams"), toks.Secrets())
	}
	if _, err := toks.Token(context.Background(), "teams"); err == nil {
		t.Error("a token after Drop")
	}
}

// A sign-in that could write gets no workload token: nothing is asked of
// Entra and every resource says why.
func TestTokensRefusedForWriter(t *testing.T) {
	fe := &fakeEntra{claims: readerClaims(RoleGlobalAdmin)}
	for name, sess := range map[string]Session{
		"admin":    NewSession(testTenant, "a", nil, readerClaims(RoleGlobalAdmin)),
		"no roles": NewSession(testTenant, "a", nil, readerClaims()),
	} {
		toks := NewTokens(sess, mainClient, &auth.Token{Refresh: "r"}, fe.source())
		toks.Acquire(context.Background(), testResources, "https://x-admin.sharepoint.com", nil)
		if len(fe.signIns)+len(fe.redeems) > 0 {
			t.Fatalf("%s: Entra was asked: %v %v", name, fe.signIns, fe.redeems)
		}
		for _, r := range testResources {
			if toks.Has(r.Name) || !strings.Contains(toks.Reason(r.Name), "no read-only") {
				t.Errorf("%s %s: reason %q", name, r.Name, toks.Reason(r.Name))
			}
		}
		if !strings.Contains(toks.ProviderReason("exchange"), "role") {
			t.Errorf("%s: provider reason %q", name, toks.ProviderReason("exchange"))
		}
	}
}

// Every token must be for the account that signed in first, in the same
// tenant, holding no role that can write; otherwise it is dropped.
func TestTokensMustBeSameReader(t *testing.T) {
	cases := map[string]struct {
		claims map[string]any
		want   string
	}{
		"other account": {map[string]any{"tid": testTenant, "oid": "00000000-0000-4000-8000-0000000000cc", "upn": "admin@fernhill.example", "wids": []any{RoleGlobalReader}}, "another account (admin@fernhill.example)"},
		"other tenant":  {map[string]any{"tid": "00000000-0000-4000-8000-0000000000dd", "oid": readerOID}, "not the audited tenant"},
		"writer role":   {readerClaims(RoleGlobalReader, "29232cdf-9323-42fd-ade2-1d097af3e4de"), "Exchange Administrator"},
	}
	for name, c := range cases {
		fe := &fakeEntra{claims: c.claims}
		toks := NewTokens(readerSession(), mainClient, nil, fe.source())
		toks.Acquire(context.Background(), testResources[:1], "", nil)
		if toks.Has("exchange") || !strings.Contains(toks.Reason("exchange"), c.want) {
			t.Errorf("%s: has=%v reason %q", name, toks.Has("exchange"), toks.Reason("exchange"))
		}
	}
	// A resource on the first sign-in's client needs its refresh token, and
	// the SharePoint resource needs the admin host.
	toks := NewTokens(readerSession(), mainClient, &auth.Token{}, (&fakeEntra{claims: readerClaims(RoleGlobalReader)}).source())
	toks.Acquire(context.Background(), testResources[2:3], "", nil)
	if !strings.Contains(toks.Reason("sharepoint"), "admin host is not known") {
		t.Errorf("sharepoint reason %q", toks.Reason("sharepoint"))
	}
	toks.Acquire(context.Background(), testResources[2:3], "https://x-admin.sharepoint.com", nil)
	if !strings.Contains(toks.Reason("sharepoint"), "no refresh token") {
		t.Errorf("sharepoint reason %q", toks.Reason("sharepoint"))
	}
	// An Entra refusal is kept as the reason.
	src := TokenSource{SignIn: func(context.Context, string, []string, string) (*auth.Token, error) {
		return nil, &auth.OAuthError{Code: "invalid_client", Description: "AADSTS700016: application not found"}
	}}
	toks = NewTokens(readerSession(), mainClient, nil, src)
	toks.Acquire(context.Background(), testResources[3:], "", nil)
	if r := toks.Reason("teams"); !strings.Contains(r, "AADSTS700016") || !strings.Contains(r, "Microsoft Teams") {
		t.Errorf("teams reason %q", r)
	}
}

func TestResourcesFile(t *testing.T) {
	s, err := LoadScopes(filepath.Join(repoRoot, ScopesFile))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Resource{}
	for _, r := range s.Resources {
		got[r.Name] = r
	}
	for name, cred := range map[string]string{"exchange": "app", "compliance": "ipps", "sharepoint": "app", "teams": "teams"} {
		if got[name].Credential != cred || !TokenProviders[got[name].Provider] {
			t.Errorf("resource %s = %+v", name, got[name])
		}
	}
	if got["exchange"].Client != got["compliance"].Client {
		t.Error("Exchange and Security & Compliance should share one client (one sign-in)")
	}
	for _, bad := range [][]Resource{
		{{Name: "x", Provider: "exchange", Resource: "http://outlook.office365.com", Credential: "app"}},
		{{Name: "x", Provider: "exchange", Resource: "https://outlook.office365.com", Credential: "graph"}},
		{{Name: "x", Provider: "exchange", Resource: "https://outlook.office365.com", Credential: "app", Client: "graph-tools"}},
		{{Name: "x", Provider: "exchange", Resource: "https://a.example", Credential: "app"}, {Name: "y", Provider: "exchange", Resource: "https://b.example", Credential: "app"}},
		{{Name: "x", Provider: "exchange", Resource: "https://a.example", Credential: "app"}, {Name: "x", Provider: "teams", Resource: "https://b.example", Credential: "teams"}},
	} {
		if CheckResources(bad) == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestStepResource(t *testing.T) {
	for _, c := range [][3]string{{"exchange", "policy.purview", "compliance"}, {"exchange", "policy.exchange", "exchange"}, {"exchange", "policy.defender", "exchange"},
		{"sharepoint", "policy.sharepoint", "sharepoint"}, {"teams", "policy.teams", "teams"}} {
		if got := StepResource(c[0], c[1]); got != c[2] {
			t.Errorf("StepResource(%s, %s) = %s", c[0], c[1], got)
		}
	}
}
