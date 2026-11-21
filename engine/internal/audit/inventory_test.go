package audit

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/audit/audittest"
	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
)

var testNow = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

func testReader(t *testing.T) (*Reader, *audittest.Graph) {
	t.Helper()
	fg := audittest.NewGraph(t)
	tok := audittest.Token(map[string]any{"tid": audittest.Tenant})
	gc := &graph.Client{HTTP: ReadOnlyClient(fg.Server.Client()), Token: func() string { return tok }}
	p, err := ParsePeriod("2026-01-01..2026-11-21", testNow)
	if err != nil {
		t.Fatal(err)
	}
	dns := audittest.Resolver{"fernhill.example": {"v=spf1 include:spf.protection.outlook.com -all"}, "_dmarc.fernhill.example": {"v=DMARC1; p=none"}}
	return &Reader{Graph: gc, V1: fg.V1(), Beta: fg.Beta(), DNS: dns, Period: p, Now: func() time.Time { return testNow }}, fg
}

func byID(invs []*Inventory) map[string]*Inventory {
	m := map[string]*Inventory{}
	for _, i := range invs {
		m[i.ID] = i
	}
	return m
}

func failing(inv *Inventory) []string {
	var out []string
	for _, c := range inv.Checks {
		if !c.OK {
			out = append(out, c.Severity+": "+c.Text)
		}
	}
	return out
}

func TestInventoriesFromRecordedGraph(t *testing.T) {
	r, fg := testReader(t)
	invs := byID(r.ReadAll(context.Background()))

	if m := fg.Methods(); len(m) != 1 || m["GET"] == 0 {
		t.Errorf("methods sent = %v, want GET only", m)
	}
	for id, inv := range invs {
		if inv.ReadAt.IsZero() || inv.Status == "" || (inv.Status != NotRead && len(inv.Queries) == 0) {
			t.Errorf("%s: missing stamp: %+v", id, inv)
		}
		if inv.Status == NotRead && inv.Reason == "" {
			t.Errorf("%s: not read without a reason", id)
		}
	}
	want := map[string]string{
		"organization": Read, "users": Read, "mfa_registration": Read, "admin_roles": Read, "ca_policies": Read,
		"devices": Read, "sharing_settings": Read, "labels": NotRead, "risky_users": NotRead, "retention": PartlyRead,
		"directory_audits": PartlyRead, "sign_ins": PartlyRead, "alerts": Read, "forwarding": NotRead, "dlp": NotRead, "mail_auth": PartlyRead,
	}
	for id, st := range want {
		if invs[id] == nil || invs[id].Status != st {
			t.Errorf("%s status = %v, want %s", id, invs[id], st)
		}
	}
	if !strings.Contains(invs["labels"].Reason, "HTTP 403") || !strings.Contains(invs["labels"].Reason, "InformationProtectionPolicy.Read") {
		t.Errorf("labels reason = %q", invs["labels"].Reason)
	}
	if !strings.Contains(invs["forwarding"].Reason, "Exchange Online PowerShell") {
		t.Errorf("forwarding reason = %q", invs["forwarding"].Reason)
	}
	// Paging: both pages of the directory audit are read.
	if n := len(invs["directory_audits"].Rows); n != 2 {
		t.Errorf("directory audit rows = %d, want 2 (two pages)", n)
	}
	// Conclusions drawn from the reads.
	mfa := strings.Join(failing(invs["mfa_registration"]), "|")
	if !strings.Contains(mfa, "1 of 3 member account(s) not registered") || strings.Contains(mfa, "administrator(s) not registered") {
		t.Errorf("mfa findings = %s", mfa)
	}
	ca := invs["ca_policies"]
	if len(failing(ca)) != 1 || !strings.Contains(failing(ca)[0], "blocks legacy authentication") {
		t.Errorf("ca findings = %v", failing(ca))
	}
	okMFA := false
	for _, c := range ca.Checks {
		okMFA = okMFA || (c.OK && strings.Contains(c.Text, "Require MFA for everyone"))
	}
	if !okMFA {
		t.Errorf("ca checks = %+v", ca.Checks)
	}
	for id, text := range map[string]string{
		"auth_methods": "phishable methods enabled: Sms", "sharing_settings": "externalUserAndGuestSharing",
		"devices": "1 of 2 device(s) not encrypted", "sign_ins": "1 successful sign-in(s) with legacy",
		"alerts": "1 high-severity alert(s)", "mail_auth": "without a DMARC policy", "organization": "consent to applications themselves: true",
		"app_registrations": "1 client secret", "guests": "1 enabled guest(s) have not signed in",
	} {
		if !strings.Contains(strings.Join(failing(invs[id]), "|"), text) {
			t.Errorf("%s findings = %v, want %q", id, failing(invs[id]), text)
		}
	}
	// Never kept: a guest's address, a secret hint, an IP address.
	for _, row := range invs["guests"].Rows {
		if _, ok := row["mail"]; ok || row["domain"] != "partner.example" {
			t.Errorf("guest row = %v", row)
		}
	}
	for _, row := range invs["app_registrations"].Rows {
		for k := range row {
			if strings.Contains(strings.ToLower(k), "hint") || k == "passwordCredentials" {
				t.Errorf("app row keeps %s", k)
			}
		}
	}
	for _, row := range invs["sign_ins"].Rows {
		if _, ok := row["ipAddress"]; ok {
			t.Error("sign-in row keeps the IP address")
		}
	}
	if r.InitialDomain() != "fernhilllabs.onmicrosoft.com" || r.OrganisationName() != "Fernhill Labs" {
		t.Errorf("initial domain %q, organisation %q", r.InitialDomain(), r.OrganisationName())
	}
}

func TestActivityCap(t *testing.T) {
	r, _ := testReader(t)
	r.Cap = 1
	inv := byID(r.ReadAll(context.Background()))["directory_audits"]
	if len(inv.Rows) != 1 || !inv.Truncated || inv.Status != PartlyRead || !strings.Contains(inv.Reason, "capped at 1") {
		t.Errorf("capped read = %+v", inv)
	}
}

// The audit's own Graph client cannot send anything but GET.
func TestGetOnlyTransport(t *testing.T) {
	fg := audittest.NewGraph(t)
	hc := ReadOnlyClient(fg.Server.Client())
	for _, m := range []string{http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete} {
		req, _ := http.NewRequest(m, fg.V1()+"/users", nil)
		if _, err := hc.Do(req); err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Errorf("%s: err = %v", m, err)
		}
	}
	gc := &graph.Client{HTTP: hc, Token: func() string { return "x" }}
	if err := gc.Do(context.Background(), http.MethodPatch, fg.V1()+"/policies/authorizationPolicy", map[string]any{"allowInvitesFrom": "none"}, nil); err == nil {
		t.Error("PATCH through the audit client succeeded")
	}
	if len(fg.Requests) != 0 {
		t.Errorf("refused requests reached the server: %v", fg.Requests)
	}
}

func TestParsePeriod(t *testing.T) {
	p, err := ParsePeriod("", testNow)
	if err != nil || p.String() != "2025-11-22..2026-11-21" {
		t.Errorf("default period = %s %v", p, err)
	}
	if p, _ := ParsePeriod("2026-01-01..2026-12-31", testNow); p.To.Format("2006-01-02") != "2026-11-21" {
		t.Errorf("future end not clipped: %s", p)
	}
	for _, bad := range []string{"2026-01-01", "2026-13-01..2026-12-01", "2026-05-01..2026-04-01"} {
		if _, err := ParsePeriod(bad, testNow); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
