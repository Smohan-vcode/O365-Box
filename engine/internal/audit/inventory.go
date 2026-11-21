package audit

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
)

// Inventory statuses.
const (
	Read       = "read"
	PartlyRead = "partly read"
	NotRead    = "not read"
)

// Finding severities of a check that does not hold.
const (
	High   = "high"
	Medium = "medium"
	Low    = "low"
)

// Check is one conclusion drawn from an inventory: OK is evidence a
// requirement is met, !OK a finding against it.
type Check struct {
	OK       bool   `json:"ok"`
	Severity string `json:"severity,omitempty"` // high | medium | low when !OK
	Text     string `json:"text"`
	// Collectors limits the check to requirements with these evidence
	// collectors; empty means every collector the inventory answers.
	Collectors []string `json:"collectors,omitempty"`
}

// checkFor adds a check that bears only on the named collectors.
func (inv *Inventory) checkFor(collectors []string, ok bool, severity, format string, a ...any) {
	inv.check(ok, severity, format, a...)
	inv.Checks[len(inv.Checks)-1].Collectors = collectors
}

// Inventory is one read of the tenant: what was asked, when, and the answer.
type Inventory struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Collectors []string  `json:"collectors"` // the requirement evidence collectors it answers (policy/requirements.yaml)
	Queries    []string  `json:"queries"`
	Status     string    `json:"status"`
	Reason     string    `json:"reason,omitempty"`
	ReadAt     time.Time `json:"read_at"`
	Count      int       `json:"count"`
	Truncated  bool      `json:"truncated,omitempty"`
	Summary    string    `json:"summary"`
	Checks     []Check   `json:"checks,omitempty"`
	// Rows is the inventory as a table (written as CSV too); Data holds
	// the other reads, by query name.
	Rows []map[string]any `json:"rows,omitempty"`
	Data map[string]any   `json:"data,omitempty"`
}

func (inv *Inventory) check(ok bool, severity, format string, a ...any) {
	c := Check{OK: ok, Text: fmt.Sprintf(format, a...)}
	if !ok {
		c.Severity = severity
	}
	inv.Checks = append(inv.Checks, c)
}

// Period is the audit period, both days included.
type Period struct{ From, To time.Time }

// ParsePeriod reads "2026-01-01..2026-11-21". Empty means the twelve
// months up to today.
func ParsePeriod(s string, now time.Time) (Period, error) {
	day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }
	if s == "" {
		to := day(now.UTC())
		return Period{From: to.AddDate(-1, 0, 1), To: to}, nil
	}
	a, b, ok := strings.Cut(s, "..")
	if !ok {
		return Period{}, fmt.Errorf("period %q is not FROM..TO (for example 2026-01-01..2026-11-21)", s)
	}
	from, err := time.Parse("2006-01-02", strings.TrimSpace(a))
	if err != nil {
		return Period{}, fmt.Errorf("period start: %w", err)
	}
	to, err := time.Parse("2006-01-02", strings.TrimSpace(b))
	if err != nil {
		return Period{}, fmt.Errorf("period end: %w", err)
	}
	if to.Before(from) {
		return Period{}, fmt.Errorf("period ends before it starts")
	}
	if today := day(now.UTC()); to.After(today) {
		to = today
	}
	return Period{From: from, To: to}, nil
}

func (p Period) String() string {
	return p.From.Format("2006-01-02") + ".." + p.To.Format("2006-01-02")
}

// end is the first instant after the period.
func (p Period) end() time.Time { return p.To.AddDate(0, 0, 1) }

// Resolver looks up DNS TXT records (net.DefaultResolver in use).
type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// Reader reads the inventories over Microsoft Graph, GET only.
type Reader struct {
	Graph    *graph.Client
	V1, Beta string // Graph bases; default https://graph.microsoft.com/{v1.0,beta}
	DNS      Resolver
	Period   Period
	Cap      int // most records per activity read (sign-ins, audits, alerts); default 5000
	Now      func() time.Time
	// Progress, when set, is told each inventory as it starts.
	Progress func(id, title string)

	org     map[string]any
	domains []map[string]any
	mfa     map[string]bool // user principal name -> MFA registered
}

func (r *Reader) defaults() {
	if r.V1 == "" {
		r.V1 = "https://graph.microsoft.com/v1.0"
	}
	if r.Beta == "" {
		r.Beta = "https://graph.microsoft.com/beta"
	}
	if r.Cap <= 0 {
		r.Cap = 5000
	}
	if r.Now == nil {
		r.Now = time.Now
	}
}

func (r *Reader) get(ctx context.Context, path string, out any) error {
	return r.Graph.Get(ctx, r.url(path), out)
}

func (r *Reader) url(path string) string {
	if strings.HasPrefix(path, "https://") {
		return path
	}
	if strings.HasPrefix(path, "/beta/") {
		return r.Beta + strings.TrimPrefix(path, "/beta")
	}
	return r.V1 + path
}

// list reads every page of path, up to max records (0: no limit).
func (r *Reader) list(ctx context.Context, path string, max int) ([]map[string]any, bool, error) {
	var all []map[string]any
	next := r.url(path)
	for next != "" {
		var page struct {
			Value    []map[string]any `json:"value"`
			NextLink string           `json:"@odata.nextLink"`
		}
		if err := r.Graph.Get(ctx, next, &page); err != nil {
			return all, false, err
		}
		all = append(all, page.Value...)
		if max > 0 && len(all) >= max {
			return all[:max], page.NextLink != "" || len(all) > max, nil
		}
		next = page.NextLink
	}
	return all, false, nil
}

// why turns a read error into the plain reason given in the pack.
func why(err error, needs string) string {
	var ge *graph.Error
	if errors.As(err, &ge) {
		switch {
		case ge.Status == 401 || ge.Status == 403:
			return fmt.Sprintf("access refused (HTTP %d %s); needs %s", ge.Status, ge.Code, needs)
		case ge.Status == 400 && strings.Contains(strings.ToLower(ge.Message), "licen"):
			return fmt.Sprintf("not available on this tenant's licences (%s)", ge.Message)
		case ge.Status == 404:
			return fmt.Sprintf("not found on this tenant (HTTP 404 %s)", ge.Code)
		}
		return fmt.Sprintf("HTTP %d %s: %s", ge.Status, ge.Code, ge.Message)
	}
	return err.Error()
}

// pick keeps only the named fields; a dotted name reads a nested field.
func pick(m map[string]any, fields ...string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		var v any = m
		for _, k := range strings.Split(f, ".") {
			mm, _ := v.(map[string]any)
			v = mm[k]
		}
		out[f] = v
	}
	return out
}

func str(m map[string]any, path string) string {
	var v any = m
	for _, k := range strings.Split(path, ".") {
		mm, _ := v.(map[string]any)
		v = mm[k]
	}
	s, _ := v.(string)
	return s
}

func boolean(m map[string]any, key string) bool { b, _ := m[key].(bool); return b }

func strs(v any) []string {
	l, _ := v.([]any)
	out := make([]string, 0, len(l))
	for _, x := range l {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

func q(s string) string { return url.QueryEscape(s) }

// ReadAll reads every inventory. A read that fails is kept with status
// "not read" and the reason; nothing is invented in its place.
func (r *Reader) ReadAll(ctx context.Context) []*Inventory {
	r.defaults()
	type reader struct {
		id, title  string
		collectors []string
		fn         func(context.Context, *Inventory)
	}
	readers := []reader{
		{"organization", "Organisation and directory settings", []string{"org_settings"}, r.organization},
		{"domains", "Domains and password expiry", []string{"password_policy"}, r.domainsInv},
		{"users", "Users and last sign-in", nil, r.users},
		{"mfa_registration", "MFA registration per user", []string{"mfa_registration"}, r.mfaRegistration},
		{"admin_roles", "Directory role assignments", []string{"admin_roles"}, r.adminRoles},
		{"guests", "Guest accounts", []string{"guests"}, r.guests},
		{"ca_policies", "Conditional Access policies and security defaults", []string{"ca_policies"}, r.caPolicies},
		{"auth_methods", "Authentication methods policy", []string{"auth_methods"}, r.authMethods},
		{"device_settings", "Device registration policy", []string{"device_settings"}, r.deviceSettings},
		{"app_registrations", "App registrations and credential expiry", []string{"app_registrations"}, r.appRegistrations},
		{"app_consents", "Delegated permission grants (app consents)", []string{"app_consents"}, r.appConsents},
		{"devices", "Managed devices and compliance", []string{"device_inventory", "update_status", "defender_health"}, r.devices},
		{"device_compliance", "Device compliance policies", []string{"device_compliance"}, r.compliancePolicies},
		{"device_config", "Device configuration and settings catalogue policies", []string{"device_config", "office_policy"}, r.deviceConfig},
		{"app_protection", "App protection policies", []string{"app_protection"}, r.appProtection},
		{"laps", "Local administrator password backups (dates only)", []string{"laps"}, r.laps},
		{"sharing_settings", "SharePoint and OneDrive sharing settings", []string{"sharing_settings"}, r.sharing},
		{"labels", "Sensitivity labels", []string{"labels"}, r.labels},
		{"retention", "Retention labels", []string{"retention"}, r.retention},
		{"mail_auth", "Mail authentication records (SPF, DMARC) in public DNS", []string{"mail_auth"}, r.mailAuth},
		{"alerts", "Security alerts over the period", nil, r.alerts},
		{"directory_audits", "Directory audit (admin changes) over the period", []string{"audit_logs"}, r.directoryAudits},
		{"sign_ins", "Sign-ins over the period", []string{"audit_logs"}, r.signIns},
		{"risky_users", "Users at risk (Entra ID Protection)", nil, r.riskyUsers},
		{"secure_score", "Microsoft Secure Score", nil, r.secureScore},
		{"licences", "Licences (subscribed SKUs)", nil, r.licences},
	}
	var out []*Inventory
	for _, rd := range readers {
		if r.Progress != nil {
			r.Progress(rd.id, rd.title)
		}
		inv := &Inventory{ID: rd.id, Title: rd.title, Collectors: rd.collectors, Status: Read, ReadAt: r.Now().UTC()}
		if err := ctx.Err(); err != nil {
			inv.Status, inv.Reason = NotRead, err.Error()
		} else {
			rd.fn(ctx, inv)
		}
		if inv.Count == 0 && len(inv.Rows) > 0 {
			inv.Count = len(inv.Rows)
		}
		out = append(out, inv)
	}
	out = append(out, NotReadInventories(r.Now().UTC())...)
	r.crossChecks(out)
	return out
}

// fail marks inv not read (or partly read when something was read).
func fail(inv *Inventory, err error, needs string) {
	reason := why(err, needs)
	if inv.Reason != "" {
		reason = inv.Reason + "; " + reason
	}
	inv.Reason = reason
	if len(inv.Rows) > 0 || len(inv.Data) > 0 {
		inv.Status = PartlyRead
	} else {
		inv.Status = NotRead
	}
}

func (inv *Inventory) put(key string, v any) {
	if inv.Data == nil {
		inv.Data = map[string]any{}
	}
	inv.Data[key] = v
}

func (r *Reader) organization(ctx context.Context, inv *Inventory) {
	p := "/organization?$select=id,displayName,verifiedDomains,createdDateTime,onPremisesSyncEnabled"
	inv.Queries = append(inv.Queries, "GET "+p)
	var org struct {
		Value []map[string]any `json:"value"`
	}
	if err := r.get(ctx, p, &org); err != nil {
		fail(inv, err, "Organization.Read.All")
		return
	}
	if len(org.Value) > 0 {
		r.org = org.Value[0]
		inv.put("organization", r.org)
	}
	for _, sub := range []struct{ key, path string }{
		{"authorization_policy", "/policies/authorizationPolicy"},
		{"security_defaults", "/policies/identitySecurityDefaultsEnforcementPolicy"},
	} {
		inv.Queries = append(inv.Queries, "GET "+sub.path)
		var v map[string]any
		if err := r.get(ctx, sub.path, &v); err != nil {
			fail(inv, err, "Policy.Read.All")
			continue
		}
		delete(v, "@odata.context")
		inv.put(sub.key, v)
	}
	ap, _ := inv.Data["authorization_policy"].(map[string]any)
	if ap != nil {
		perm, _ := ap["defaultUserRolePermissions"].(map[string]any)
		inv.check(!boolean(perm, "allowedToCreateApps"), Low, "users can register applications: %v", boolean(perm, "allowedToCreateApps"))
		grants := strs(perm["permissionGrantPoliciesAssigned"])
		userConsent := false
		for _, g := range grants {
			userConsent = userConsent || strings.Contains(g, "microsoft-user-default")
		}
		inv.check(!userConsent, Medium, "users can consent to applications themselves: %v", userConsent)
		inv.check(str(ap, "allowInvitesFrom") != "everyone", Low, "who can invite guests: %s", str(ap, "allowInvitesFrom"))
	}
	inv.Summary = fmt.Sprintf("organisation, authorization policy and security defaults (%d read)", len(inv.Data))
}

func (r *Reader) domainsInv(ctx context.Context, inv *Inventory) {
	p := "/domains?$select=id,isVerified,isDefault,isInitial,authenticationType,passwordValidityPeriodInDays"
	inv.Queries = []string{"GET " + p}
	rows, _, err := r.list(ctx, p, 0)
	if err != nil {
		fail(inv, err, "Domain.Read.All")
		return
	}
	r.domains = rows
	for _, d := range rows {
		inv.Rows = append(inv.Rows, pick(d, "id", "isVerified", "isDefault", "isInitial", "authenticationType", "passwordValidityPeriodInDays"))
	}
	expiring := 0
	for _, d := range rows {
		if v, ok := d["passwordValidityPeriodInDays"].(float64); ok && v < 2147483647 {
			expiring++
		}
	}
	inv.check(expiring == 0, Low, "%d domain(s) set passwords to expire (current guidance: never expire, with MFA)", expiring)
	inv.Summary = fmt.Sprintf("%d domain(s)", len(rows))
}

func lastSignIn(u map[string]any) string {
	sa, _ := u["signInActivity"].(map[string]any)
	a, b := str(sa, "lastSignInDateTime"), str(sa, "lastNonInteractiveSignInDateTime")
	if b > a {
		return b
	}
	return a
}

func (r *Reader) users(ctx context.Context, inv *Inventory) {
	base := "/users?$top=999&$select=id,displayName,userPrincipalName,userType,accountEnabled,createdDateTime,assignedLicenses"
	p := base + ",signInActivity"
	inv.Queries = []string{"GET " + p}
	rows, trunc, err := r.list(ctx, p, 0)
	withSignIn := err == nil
	if err != nil {
		// signInActivity needs AuditLog.Read.All and Entra ID P1.
		inv.Reason = "last sign-in not read: " + why(err, "AuditLog.Read.All and Entra ID P1")
		inv.Queries = append(inv.Queries, "GET "+base)
		if rows, trunc, err = r.list(ctx, base, 0); err != nil {
			fail(inv, err, "User.Read.All")
			return
		}
		inv.Status = PartlyRead
	}
	inv.Truncated = trunc
	stale, cutoff := 0, r.Now().UTC().AddDate(0, 0, -90).Format(time.RFC3339)
	for _, u := range rows {
		if strings.EqualFold(str(u, "userType"), "Guest") {
			continue // in the guests inventory, without their address
		}
		lic, _ := u["assignedLicenses"].([]any)
		row := pick(u, "id", "displayName", "userPrincipalName", "accountEnabled", "createdDateTime")
		row["licences"] = len(lic)
		if withSignIn {
			ls := lastSignIn(u)
			row["lastSignIn"] = ls
			if boolean(u, "accountEnabled") && ls != "" && ls < cutoff {
				stale++
			}
		}
		inv.Rows = append(inv.Rows, row)
	}
	if withSignIn {
		inv.check(stale == 0, Low, "%d enabled member account(s) have not signed in for 90 days", stale)
	}
	inv.Summary = fmt.Sprintf("%d member account(s)", len(inv.Rows))
}

func (r *Reader) mfaRegistration(ctx context.Context, inv *Inventory) {
	p := "/reports/authenticationMethods/userRegistrationDetails?$top=999"
	inv.Queries = []string{"GET " + p}
	rows, trunc, err := r.list(ctx, p, 0)
	if err != nil {
		fail(inv, err, "AuditLog.Read.All or Reports.Read.All, and Global Reader or Reports Reader")
		return
	}
	inv.Truncated = trunc
	r.mfa = map[string]bool{}
	members, notReg, adminsNot := 0, 0, 0
	for _, u := range rows {
		if strings.EqualFold(str(u, "userType"), "guest") {
			continue
		}
		members++
		reg := boolean(u, "isMfaRegistered")
		r.mfa[strings.ToLower(str(u, "userPrincipalName"))] = reg
		if !reg {
			notReg++
			if boolean(u, "isAdmin") {
				adminsNot++
			}
		}
		inv.Rows = append(inv.Rows, pick(u, "userPrincipalName", "userDisplayName", "isAdmin", "isMfaRegistered", "isMfaCapable",
			"isPasswordlessCapable", "isSsprRegistered", "methodsRegistered", "lastUpdatedDateTime"))
	}
	inv.check(adminsNot == 0, High, "%d administrator(s) not registered for MFA", adminsNot)
	inv.check(notReg == 0, Medium, "%d of %d member account(s) not registered for MFA", notReg, members)
	inv.Summary = fmt.Sprintf("%d of %d member account(s) registered for MFA", members-notReg, members)
}

func (r *Reader) adminRoles(ctx context.Context, inv *Inventory) {
	pd := "/roleManagement/directory/roleDefinitions?$select=id,displayName,templateId,isBuiltIn"
	pa := "/roleManagement/directory/roleAssignments?$expand=principal"
	inv.Queries = []string{"GET " + pd, "GET " + pa}
	defs, _, err := r.list(ctx, pd, 0)
	if err != nil {
		fail(inv, err, "RoleManagement.Read.Directory")
		return
	}
	names := map[string]string{}
	for _, d := range defs {
		names[str(d, "id")] = str(d, "displayName")
		names[str(d, "templateId")] = str(d, "displayName")
	}
	asg, trunc, err := r.list(ctx, pa, 0)
	if err != nil {
		fail(inv, err, "RoleManagement.Read.Directory")
		return
	}
	inv.Truncated = trunc
	ga := 0
	for _, a := range asg {
		p, _ := a["principal"].(map[string]any)
		role := str(a, "roleDefinitionId")
		row := map[string]any{"role": names[role], "roleDefinitionId": role, "directoryScopeId": str(a, "directoryScopeId"),
			"principalId": str(a, "principalId"), "principalType": strings.TrimPrefix(str(p, "@odata.type"), "#microsoft.graph."),
			"principal": str(p, "displayName"), "userPrincipalName": str(p, "userPrincipalName")}
		if role == RoleGlobalAdmin {
			ga++
		}
		inv.Rows = append(inv.Rows, row)
	}
	pe := "/roleManagement/directory/roleEligibilitySchedules"
	inv.Queries = append(inv.Queries, "GET "+pe)
	if el, _, err := r.list(ctx, pe, 0); err == nil {
		var rows []map[string]any
		for _, e := range el {
			rows = append(rows, map[string]any{"role": names[str(e, "roleDefinitionId")], "principalId": str(e, "principalId"),
				"memberType": str(e, "memberType"), "status": str(e, "status")})
		}
		inv.put("eligible", rows)
	} else {
		inv.put("eligible_not_read", why(err, "Entra ID P2 (Privileged Identity Management)"))
	}
	inv.check(ga >= 2 && ga <= 4, Medium, "%d Global Administrator assignment(s) (between 2 and 4 expected, including break-glass)", ga)
	inv.Summary = fmt.Sprintf("%d role assignment(s), %d Global Administrator", len(asg), ga)
}

func (r *Reader) guests(ctx context.Context, inv *Inventory) {
	base := "/users?$top=999&$filter=" + q("userType eq 'Guest'") + "&$select=id,displayName,mail,createdDateTime,externalUserState,accountEnabled"
	p := base + ",signInActivity"
	inv.Queries = []string{"GET " + p}
	rows, trunc, err := r.list(ctx, p, 0)
	withSignIn := err == nil
	if err != nil {
		inv.Reason = "last sign-in not read: " + why(err, "AuditLog.Read.All and Entra ID P1")
		inv.Queries = append(inv.Queries, "GET "+base)
		if rows, trunc, err = r.list(ctx, base, 0); err != nil {
			fail(inv, err, "User.Read.All")
			return
		}
		inv.Status = PartlyRead
	}
	inv.Truncated = trunc
	stale, cutoff := 0, r.Now().UTC().AddDate(0, 0, -90).Format(time.RFC3339)
	for _, u := range rows {
		// A guest's address can be personal: only its domain is kept.
		_, dom, _ := strings.Cut(str(u, "mail"), "@")
		row := map[string]any{"id": str(u, "id"), "displayName": str(u, "displayName"), "domain": strings.ToLower(dom),
			"createdDateTime": str(u, "createdDateTime"), "externalUserState": str(u, "externalUserState"), "accountEnabled": boolean(u, "accountEnabled")}
		if withSignIn {
			ls := lastSignIn(u)
			row["lastSignIn"] = ls
			if boolean(u, "accountEnabled") && (ls == "" || ls < cutoff) && str(u, "createdDateTime") < cutoff {
				stale++
			}
		}
		inv.Rows = append(inv.Rows, row)
	}
	if withSignIn {
		inv.check(stale == 0, Low, "%d enabled guest(s) have not signed in for 90 days", stale)
	}
	inv.Summary = fmt.Sprintf("%d guest account(s)", len(inv.Rows))
}

func (r *Reader) caPolicies(ctx context.Context, inv *Inventory) {
	p := "/identity/conditionalAccess/policies"
	ps := "/policies/identitySecurityDefaultsEnforcementPolicy"
	inv.Queries = []string{"GET " + p, "GET " + ps}
	var sd map[string]any
	sdOn := false
	if err := r.get(ctx, ps, &sd); err == nil {
		sdOn = boolean(sd, "isEnabled")
		inv.put("security_defaults_enabled", sdOn)
	}
	rows, _, err := r.list(ctx, p, 0)
	if err != nil {
		fail(inv, err, "Policy.Read.All and Entra ID P1")
		if sdOn {
			inv.check(true, "", "security defaults are on (MFA for everyone, legacy sign-in blocked)")
		}
		return
	}
	mfaAll, legacyBlocked, adminMFA := "", "", ""
	enabled, reportOnly := 0, 0
	for _, c := range rows {
		state := str(c, "state")
		cond, _ := c["conditions"].(map[string]any)
		users, _ := cond["users"].(map[string]any)
		apps, _ := cond["applications"].(map[string]any)
		grant, _ := c["grantControls"].(map[string]any)
		controls := strs(grant["builtInControls"])
		strength, _ := grant["authenticationStrength"].(map[string]any)
		requiresMFA := contains(controls, "mfa") || strength != nil
		allUsers := contains(strs(users["includeUsers"]), "All")
		allApps := contains(strs(apps["includeApplications"]), "All")
		switch state {
		case "enabled":
			enabled++
		case "enabledForReportingButNotEnforced":
			reportOnly++
		}
		if state == "enabled" && allUsers && allApps && requiresMFA && mfaAll == "" {
			mfaAll = str(c, "displayName")
		}
		if state == "enabled" && requiresMFA && len(strs(users["includeRoles"])) > 0 && adminMFA == "" {
			adminMFA = str(c, "displayName")
		}
		clients := strs(cond["clientAppTypes"])
		if state == "enabled" && contains(controls, "block") && (contains(clients, "exchangeActiveSync") || contains(clients, "other")) && legacyBlocked == "" {
			legacyBlocked = str(c, "displayName")
		}
		inv.Rows = append(inv.Rows, c)
	}
	switch {
	case sdOn:
		inv.check(true, "", "security defaults are on (MFA for everyone, legacy sign-in blocked)")
	case mfaAll != "":
		inv.check(true, "", "MFA is required for all users and all apps by %q", mfaAll)
	default:
		inv.check(false, High, "no enabled policy requires MFA for all users and all apps, and security defaults are off")
	}
	if !sdOn {
		if legacyBlocked != "" {
			inv.check(true, "", "legacy authentication is blocked by %q", legacyBlocked)
		} else {
			inv.check(false, Medium, "no enabled policy blocks legacy authentication")
		}
		if adminMFA != "" {
			inv.check(true, "", "administrator roles must use MFA by %q", adminMFA)
		}
	}
	inv.Summary = fmt.Sprintf("%d policies: %d enabled, %d report-only; security defaults on: %v", len(rows), enabled, reportOnly, sdOn)
}

func (r *Reader) authMethods(ctx context.Context, inv *Inventory) {
	p := "/policies/authenticationMethodsPolicy"
	inv.Queries = []string{"GET " + p}
	var v map[string]any
	if err := r.get(ctx, p, &v); err != nil {
		fail(inv, err, "Policy.Read.All")
		return
	}
	delete(v, "@odata.context")
	inv.put("policy", v)
	cfgs, _ := v["authenticationMethodConfigurations"].([]any)
	var on, weak []string
	for _, x := range cfgs {
		m, _ := x.(map[string]any)
		if str(m, "state") == "enabled" {
			on = append(on, str(m, "id"))
			if id := strings.ToLower(str(m, "id")); id == "sms" || id == "voice" {
				weak = append(weak, str(m, "id"))
			}
		}
	}
	inv.check(len(weak) == 0, Medium, "phishable methods enabled: %s", orNone(weak))
	if contains(on, "Fido2") || contains(on, "MicrosoftAuthenticator") {
		inv.check(true, "", "strong methods enabled: %s", strings.Join(on, ", "))
	}
	inv.Summary = "enabled methods: " + orNone(on)
}

func orNone(l []string) string {
	if len(l) == 0 {
		return "none"
	}
	return strings.Join(l, ", ")
}

func (r *Reader) deviceSettings(ctx context.Context, inv *Inventory) {
	p := "/policies/deviceRegistrationPolicy"
	inv.Queries = []string{"GET " + p}
	var v map[string]any
	if err := r.get(ctx, p, &v); err != nil {
		inv.Queries = append(inv.Queries, "GET /beta"+p)
		if err2 := r.get(ctx, "/beta"+p, &v); err2 != nil {
			fail(inv, err2, "Policy.Read.All")
			return
		}
	}
	delete(v, "@odata.context")
	inv.put("policy", v)
	inv.Summary = "device registration and join settings"
}

func (r *Reader) appRegistrations(ctx context.Context, inv *Inventory) {
	p := "/applications?$top=999&$select=id,appId,displayName,createdDateTime,passwordCredentials,keyCredentials"
	inv.Queries = []string{"GET " + p}
	rows, trunc, err := r.list(ctx, p, 0)
	if err != nil {
		fail(inv, err, "Application.Read.All")
		return
	}
	inv.Truncated = trunc
	now := r.Now().UTC().Format(time.RFC3339)
	secrets, expired := 0, 0
	for _, a := range rows {
		// Only credential dates are kept: never a secret or its hint.
		ends := func(key string) (n int, last string) {
			l, _ := a[key].([]any)
			for _, c := range l {
				m, _ := c.(map[string]any)
				e := str(m, "endDateTime")
				if e != "" && e < now {
					expired++
				}
				if e > last {
					last = e
				}
				n++
			}
			return
		}
		ns, ls := ends("passwordCredentials")
		nk, lk := ends("keyCredentials")
		secrets += ns
		inv.Rows = append(inv.Rows, map[string]any{"appId": str(a, "appId"), "displayName": str(a, "displayName"), "createdDateTime": str(a, "createdDateTime"),
			"clientSecrets": ns, "lastSecretExpiry": ls, "certificates": nk, "lastCertificateExpiry": lk})
	}
	inv.check(secrets == 0, Low, "%d client secret(s) on app registrations (certificates or managed identities preferred)", secrets)
	inv.check(expired == 0, Low, "%d expired credential(s) left on app registrations", expired)
	inv.Summary = fmt.Sprintf("%d app registration(s)", len(rows))
}

func (r *Reader) appConsents(ctx context.Context, inv *Inventory) {
	p := "/oauth2PermissionGrants?$top=999"
	inv.Queries = []string{"GET " + p}
	rows, trunc, err := r.list(ctx, p, 0)
	if err != nil {
		fail(inv, err, "Directory.Read.All")
		return
	}
	inv.Truncated = trunc
	user := 0
	for _, g := range rows {
		if str(g, "consentType") == "Principal" {
			user++
		}
		inv.Rows = append(inv.Rows, pick(g, "clientId", "consentType", "principalId", "resourceId", "scope"))
	}
	inv.check(user == 0, Low, "%d grant(s) consented by users for themselves", user)
	inv.Summary = fmt.Sprintf("%d delegated grant(s), %d by users", len(rows), user)
}

var inventoryOnly = []string{"device_inventory"}

func (r *Reader) devices(ctx context.Context, inv *Inventory) {
	p := "/deviceManagement/managedDevices?$select=id,deviceName,operatingSystem,osVersion,complianceState,isEncrypted,lastSyncDateTime,managedDeviceOwnerType,userPrincipalName,enrolledDateTime"
	inv.Queries = []string{"GET " + p}
	rows, trunc, err := r.list(ctx, p, 0)
	if err != nil {
		fail(inv, err, "DeviceManagementManagedDevices.Read.All and an Intune licence")
		return
	}
	inv.Truncated = trunc
	nonc, unenc, stale := 0, 0, 0
	cutoff := r.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339)
	byOS := map[string]int{}
	for _, d := range rows {
		inv.Rows = append(inv.Rows, pick(d, "deviceName", "operatingSystem", "osVersion", "complianceState", "isEncrypted",
			"lastSyncDateTime", "managedDeviceOwnerType", "userPrincipalName", "enrolledDateTime"))
		byOS[str(d, "operatingSystem")]++
		if s := str(d, "complianceState"); s != "compliant" && s != "inGracePeriod" {
			nonc++
		}
		if !boolean(d, "isEncrypted") {
			unenc++
		}
		if str(d, "lastSyncDateTime") < cutoff {
			stale++
		}
	}
	if len(rows) == 0 {
		inv.check(false, Medium, "no devices are enrolled in Intune")
	}
	if len(rows) > 0 {
		inv.checkFor(inventoryOnly, nonc == 0, Medium, "%d of %d device(s) not compliant", nonc, len(rows))
		inv.checkFor(inventoryOnly, unenc == 0, Medium, "%d of %d device(s) not encrypted", unenc, len(rows))
		inv.checkFor(inventoryOnly, stale == 0, Low, "%d device(s) not synced for 30 days", stale)
	}
	var os []string
	for k, n := range byOS {
		os = append(os, fmt.Sprintf("%s %d", k, n))
	}
	sort.Strings(os)
	inv.Summary = fmt.Sprintf("%d managed device(s) (%s)", len(rows), orNone(os))
}

func (r *Reader) simpleList(ctx context.Context, inv *Inventory, path, needs string, fields ...string) []map[string]any {
	inv.Queries = append(inv.Queries, "GET "+path)
	rows, trunc, err := r.list(ctx, path, 0)
	if err != nil {
		fail(inv, err, needs)
		return nil
	}
	inv.Truncated = inv.Truncated || trunc
	for _, x := range rows {
		inv.Rows = append(inv.Rows, pick(x, fields...))
	}
	return rows
}

func (r *Reader) compliancePolicies(ctx context.Context, inv *Inventory) {
	rows := r.simpleList(ctx, inv, "/deviceManagement/deviceCompliancePolicies?$select=id,displayName,lastModifiedDateTime",
		"DeviceManagementConfiguration.Read.All and an Intune licence", "@odata.type", "id", "displayName", "lastModifiedDateTime")
	if inv.Status == NotRead {
		return
	}
	if len(rows) == 0 {
		inv.check(false, Medium, "no device compliance policy exists")
	}
	inv.Summary = fmt.Sprintf("%d compliance polic(ies)", len(rows))
}

func (r *Reader) deviceConfig(ctx context.Context, inv *Inventory) {
	a := r.simpleList(ctx, inv, "/deviceManagement/deviceConfigurations?$select=id,displayName,lastModifiedDateTime",
		"DeviceManagementConfiguration.Read.All and an Intune licence", "@odata.type", "id", "displayName", "lastModifiedDateTime")
	b := r.simpleList(ctx, inv, "/beta/deviceManagement/configurationPolicies?$select=id,name,platforms,technologies,lastModifiedDateTime",
		"DeviceManagementConfiguration.Read.All and an Intune licence", "id", "name", "platforms", "technologies", "lastModifiedDateTime")
	if inv.Status == NotRead {
		return
	}
	inv.Summary = fmt.Sprintf("%d configuration profile(s), %d settings catalogue polic(ies)", len(a), len(b))
}

func (r *Reader) appProtection(ctx context.Context, inv *Inventory) {
	rows := r.simpleList(ctx, inv, "/deviceAppManagement/managedAppPolicies?$select=id,displayName,lastModifiedDateTime",
		"DeviceManagementApps.Read.All and an Intune licence", "@odata.type", "id", "displayName", "lastModifiedDateTime")
	if inv.Status == NotRead {
		return
	}
	if len(rows) == 0 {
		inv.check(false, Medium, "no app protection policy exists")
	}
	inv.Summary = fmt.Sprintf("%d app protection polic(ies)", len(rows))
}

func (r *Reader) laps(ctx context.Context, inv *Inventory) {
	rows := r.simpleList(ctx, inv, "/directory/deviceLocalCredentials?$select=id,deviceName,lastBackupDateTime",
		"DeviceLocalCredential.ReadBasic.All", "deviceName", "lastBackupDateTime")
	if inv.Status == NotRead {
		return
	}
	inv.Summary = fmt.Sprintf("%d device(s) back up a local administrator password (dates only; no password is read)", len(rows))
}

func (r *Reader) sharing(ctx context.Context, inv *Inventory) {
	p := "/admin/sharepoint/settings"
	inv.Queries = []string{"GET " + p}
	var v map[string]any
	if err := r.get(ctx, p, &v); err != nil {
		fail(inv, err, "SharePointTenantSettings.Read.All and Global Reader or SharePoint Administrator")
		return
	}
	delete(v, "@odata.context")
	inv.put("settings", v)
	cap := str(v, "sharingCapability")
	inv.check(cap != "externalUserAndGuestSharing", Medium, "external sharing: %s (anyone links allowed when externalUserAndGuestSharing)", cap)
	inv.check(!boolean(v, "isResharingByExternalUsersEnabled"), Low, "guests can reshare: %v", boolean(v, "isResharingByExternalUsersEnabled"))
	inv.Summary = "tenant sharing: " + cap
}

func (r *Reader) labels(ctx context.Context, inv *Inventory) {
	rows := r.simpleList(ctx, inv, "/beta/security/informationProtection/sensitivityLabels",
		"InformationProtectionPolicy.Read and a Purview reader role", "id", "name", "description", "isActive", "sensitivity", "parent.id")
	if inv.Status == NotRead {
		return
	}
	if len(rows) == 0 {
		inv.check(false, Medium, "no sensitivity labels are published")
	}
	inv.Summary = fmt.Sprintf("%d sensitivity label(s)", len(rows))
}

func (r *Reader) retention(ctx context.Context, inv *Inventory) {
	rows := r.simpleList(ctx, inv, "/security/labels/retentionLabels",
		"RecordsManagement.Read.All and a Purview reader role", "id", "displayName", "retentionDuration", "actionAfterRetentionPeriod", "behaviorDuringRetentionPeriod")
	if inv.Status == NotRead {
		return
	}
	inv.Reason = "retention policies (not labels) are not exposed by Microsoft Graph: read with Security & Compliance PowerShell (Get-RetentionCompliancePolicy)"
	inv.Status = PartlyRead
	inv.Summary = fmt.Sprintf("%d retention label(s)", len(rows))
}

func (r *Reader) mailAuth(ctx context.Context, inv *Inventory) {
	if r.DNS == nil {
		inv.Status, inv.Reason = NotRead, "no DNS resolver"
		return
	}
	if r.domains == nil {
		inv.Status, inv.Reason = NotRead, "the domain list was not read"
		return
	}
	noSPF, weakDMARC := 0, 0
	for _, d := range r.domains {
		name := str(d, "id")
		if !boolean(d, "isVerified") || strings.HasSuffix(name, ".onmicrosoft.com") {
			continue
		}
		inv.Queries = append(inv.Queries, "DNS TXT "+name, "DNS TXT _dmarc."+name)
		row := map[string]any{"domain": name}
		if txt, err := r.DNS.LookupTXT(ctx, name); err == nil {
			for _, t := range txt {
				if strings.HasPrefix(strings.ToLower(t), "v=spf1") {
					row["spf"] = t
				}
			}
		} else {
			row["spf_error"] = err.Error()
		}
		if txt, err := r.DNS.LookupTXT(ctx, "_dmarc."+name); err == nil {
			for _, t := range txt {
				if strings.HasPrefix(strings.ToLower(t), "v=dmarc1") {
					row["dmarc"] = t
				}
			}
		} else {
			row["dmarc_error"] = err.Error()
		}
		if row["spf"] == nil {
			noSPF++
		}
		dm, _ := row["dmarc"].(string)
		if p := dmarcPolicy(dm); p != "reject" && p != "quarantine" {
			weakDMARC++
		}
		inv.Rows = append(inv.Rows, row)
	}
	inv.check(noSPF == 0, Medium, "%d domain(s) without an SPF record", noSPF)
	inv.check(weakDMARC == 0, Medium, "%d domain(s) without a DMARC policy of quarantine or reject", weakDMARC)
	inv.Reason = "DKIM signing is read with Exchange Online PowerShell (Get-DkimSigningConfig); not read here"
	inv.Status = PartlyRead
	inv.Summary = fmt.Sprintf("%d sending domain(s) checked", len(inv.Rows))
}

func dmarcPolicy(rec string) string {
	for _, part := range strings.Split(rec, ";") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		if strings.EqualFold(k, "p") {
			return strings.ToLower(strings.TrimSpace(v))
		}
	}
	return ""
}

func (r *Reader) periodFilter(field string) string {
	return q(fmt.Sprintf("%s ge %s and %s lt %s", field, r.Period.From.Format(time.RFC3339), field, r.Period.end().Format(time.RFC3339)))
}

// activity reads a capped, time-filtered collection and notes how far
// back the tenant keeps it.
func (r *Reader) activity(ctx context.Context, inv *Inventory, path, timeField, needs, keeps string, project func(map[string]any) map[string]any) {
	inv.Queries = []string{"GET " + path}
	rows, trunc, err := r.list(ctx, path, r.Cap)
	if err != nil {
		fail(inv, err, needs)
		return
	}
	inv.Truncated = trunc
	earliest := ""
	for _, x := range rows {
		if t := str(x, timeField); earliest == "" || t < earliest {
			earliest = t
		}
		inv.Rows = append(inv.Rows, project(x))
	}
	inv.Summary = fmt.Sprintf("%d record(s) in %s", len(rows), r.Period)
	if trunc {
		inv.Summary += fmt.Sprintf(" (capped at %d, newest first)", r.Cap)
		inv.Status = PartlyRead
		inv.Reason = fmt.Sprintf("capped at %d records; run with a shorter --period or a higher --cap for the rest", r.Cap)
	} else if earliest == "" || earliest > r.Period.From.AddDate(0, 0, 1).Format(time.RFC3339) {
		inv.Status = PartlyRead
		inv.Reason = "the tenant keeps " + keeps + "; earlier parts of the period are no longer available"
		if earliest != "" {
			inv.Reason += " (earliest record " + earliest + ")"
		}
	}
}

func (r *Reader) directoryAudits(ctx context.Context, inv *Inventory) {
	p := "/auditLogs/directoryAudits?$top=999&$filter=" + r.periodFilter("activityDateTime")
	r.activity(ctx, inv, p, "activityDateTime", "AuditLog.Read.All", "directory audit for 30 days (7 without Entra ID P1)", func(x map[string]any) map[string]any {
		by, _ := x["initiatedBy"].(map[string]any)
		who := str(by, "user.userPrincipalName")
		if who == "" {
			who = str(by, "app.displayName")
		}
		var targets []string
		tl, _ := x["targetResources"].([]any)
		for _, t := range tl {
			m, _ := t.(map[string]any)
			if n := str(m, "displayName"); n != "" {
				targets = append(targets, n)
			} else if n := str(m, "userPrincipalName"); n != "" {
				targets = append(targets, n)
			}
		}
		return map[string]any{"activityDateTime": str(x, "activityDateTime"), "category": str(x, "category"),
			"activity": str(x, "activityDisplayName"), "result": str(x, "result"), "initiatedBy": who, "targets": strings.Join(targets, "; ")}
	})
}

func (r *Reader) signIns(ctx context.Context, inv *Inventory) {
	p := "/auditLogs/signIns?$top=999&$filter=" + r.periodFilter("createdDateTime")
	legacy := 0
	r.activity(ctx, inv, p, "createdDateTime", "AuditLog.Read.All and Entra ID P1", "sign-ins for 30 days (Entra ID P1)", func(x map[string]any) map[string]any {
		st, _ := x["status"].(map[string]any)
		code, _ := st["errorCode"].(float64)
		app := str(x, "clientAppUsed")
		if code == 0 && app != "" && app != "Browser" && app != "Mobile Apps and Desktop clients" {
			legacy++
		}
		return map[string]any{"createdDateTime": str(x, "createdDateTime"), "userPrincipalName": str(x, "userPrincipalName"),
			"appDisplayName": str(x, "appDisplayName"), "clientAppUsed": app, "errorCode": code,
			"conditionalAccessStatus": str(x, "conditionalAccessStatus"), "riskLevelDuringSignIn": str(x, "riskLevelDuringSignIn"), "isInteractive": x["isInteractive"]}
	})
	if inv.Status != NotRead {
		inv.check(legacy == 0, Medium, "%d successful sign-in(s) with legacy authentication clients", legacy)
	}
}

func (r *Reader) alerts(ctx context.Context, inv *Inventory) {
	p := "/security/alerts_v2?$top=999&$filter=" + r.periodFilter("createdDateTime")
	r.activity(ctx, inv, p, "createdDateTime", "SecurityAlert.Read.All and Security Reader", "alerts for 180 days", func(x map[string]any) map[string]any {
		return pick(x, "id", "title", "severity", "status", "category", "serviceSource", "createdDateTime", "resolvedDateTime")
	})
	if inv.Status == NotRead {
		return
	}
	open := 0
	for _, a := range inv.Rows {
		if a["severity"] == "high" && a["status"] != "resolved" {
			open++
		}
	}
	inv.check(open == 0, High, "%d high-severity alert(s) in the period not resolved", open)
	// The read covers the period; alerts older than the tenant keeps are
	// not a gap in the read itself.
	if inv.Status == PartlyRead && !inv.Truncated {
		inv.Status, inv.Reason = Read, ""
	}
}

func (r *Reader) riskyUsers(ctx context.Context, inv *Inventory) {
	rows := r.simpleList(ctx, inv, "/identityProtection/riskyUsers?$filter="+q("riskState eq 'atRisk'"),
		"IdentityRiskyUser.Read.All and Entra ID P2", "userPrincipalName", "riskLevel", "riskState", "riskLastUpdatedDateTime")
	if inv.Status == NotRead {
		return
	}
	high := 0
	for _, u := range rows {
		if str(u, "riskLevel") == "high" {
			high++
		}
	}
	inv.check(high == 0, High, "%d user(s) at high risk", high)
	inv.Summary = fmt.Sprintf("%d user(s) at risk", len(rows))
}

func (r *Reader) secureScore(ctx context.Context, inv *Inventory) {
	p := "/security/secureScores?$top=1"
	inv.Queries = []string{"GET " + p}
	var v struct {
		Value []map[string]any `json:"value"`
	}
	if err := r.get(ctx, p, &v); err != nil {
		fail(inv, err, "SecurityEvents.Read.All and Security Reader")
		return
	}
	if len(v.Value) == 0 {
		inv.Summary = "no score yet"
		return
	}
	s := v.Value[0]
	inv.put("score", pick(s, "createdDateTime", "currentScore", "maxScore", "licensedUserCount", "activeUserCount"))
	inv.put("controls", s["controlScores"])
	inv.Summary = fmt.Sprintf("%v of %v on %s", s["currentScore"], s["maxScore"], str(s, "createdDateTime"))
}

func (r *Reader) licences(ctx context.Context, inv *Inventory) {
	p := "/subscribedSkus"
	inv.Queries = []string{"GET " + p}
	rows, _, err := r.list(ctx, p, 0)
	if err != nil {
		fail(inv, err, "Organization.Read.All")
		return
	}
	for _, s := range rows {
		inv.Rows = append(inv.Rows, pick(s, "skuPartNumber", "skuId", "capabilityStatus", "consumedUnits", "prepaidUnits.enabled"))
	}
	inv.Summary = fmt.Sprintf("%d subscription(s)", len(rows))
}

// crossChecks draws conclusions that need two inventories.
func (r *Reader) crossChecks(all []*Inventory) {
	var roles *Inventory
	for _, inv := range all {
		if inv.ID == "admin_roles" {
			roles = inv
		}
	}
	if roles == nil || roles.Status == NotRead || r.mfa == nil {
		return
	}
	var without []string
	for _, row := range roles.Rows {
		upn, _ := row["userPrincipalName"].(string)
		if row["roleDefinitionId"] == RoleGlobalAdmin && upn != "" {
			if reg, ok := r.mfa[strings.ToLower(upn)]; ok && !reg {
				without = append(without, upn)
			}
		}
	}
	roles.check(len(without) == 0, High, "%d Global Administrator(s) not registered for MFA", len(without))
}

// InitialDomain is the tenant's onmicrosoft.com domain, once the
// organisation is read.
func (r *Reader) InitialDomain() string {
	for _, d := range r.domains {
		if boolean(d, "isInitial") {
			return str(d, "id")
		}
	}
	if r.org != nil {
		vd, _ := r.org["verifiedDomains"].([]any)
		for _, x := range vd {
			m, _ := x.(map[string]any)
			if boolean(m, "isInitial") {
				return str(m, "name")
			}
		}
	}
	return ""
}

// OrganisationName is the tenant's display name, once read.
func (r *Reader) OrganisationName() string { return str(r.org, "displayName") }

// NotReadInventories are the reads Microsoft Graph cannot make with the
// audit's sign-in, each with what reading it needs. They are listed in the
// pack as not read, never filled with assumed values.
func NotReadInventories(at time.Time) []*Inventory {
	exo := "Exchange Online PowerShell with a delegated sign-in (Global Reader or View-Only Organization Management)"
	scc := "Security & Compliance PowerShell with a delegated sign-in (Compliance Administrator or a Purview reader role)"
	box := "a Company in a Box record: the tenant's Admin and Compliance sites hold it once the box has built the tenant"
	list := []struct {
		id, title, collector, needs string
	}{
		{"forwarding", "Mailbox forwarding and the outbound forwarding policy", "forwarding", exo + ": Get-Mailbox (ForwardingSmtpAddress), Get-InboxRule, Get-HostedOutboundSpamFilterPolicy (AutoForwardingMode). Microsoft Graph reads a mailbox's rules only with access to that mailbox"},
		{"dlp", "Data loss prevention policies", "dlp", scc + ": Get-DlpCompliancePolicy, Get-DlpComplianceRule. Microsoft Graph has no DLP policy list"},
		{"audit_status", "Unified audit log status", "audit_status", exo + ": Get-AdminAuditLogConfig (UnifiedAuditLogIngestionEnabled)"},
		{"alert_policies", "Alert policies", "alert_policies", scc + ": Get-ProtectionAlert. Alerts raised in the period are in the alerts inventory"},
		{"mail_protection", "Anti-spam, anti-phishing, Safe Links and Safe Attachments", "mail_protection", exo + ": Get-AntiPhishPolicy, Get-HostedContentFilterPolicy, Get-SafeLinksPolicy, Get-SafeAttachmentPolicy"},
		{"mail_protocols", "Legacy mail protocols (SMTP AUTH, POP, IMAP)", "mail_protocols", exo + ": Get-TransportConfig, Get-CASMailbox. Sign-ins with legacy clients are in the sign-ins inventory"},
		{"shared_mailboxes", "Shared mailboxes (sign-in blocked)", "shared_mailboxes", exo + ": Get-Mailbox -RecipientTypeDetails SharedMailbox"},
		{"teams_settings", "Teams external access and meeting policies", "teams_settings", "Microsoft Teams PowerShell (Teams Administrator or Global Reader): Get-CsTenantFederationConfiguration, Get-CsTeamsMeetingPolicy"},
		{"power_platform", "Power Platform environments and data policies", "power_platform", "the Power Platform admin API with a Power Platform administrator sign-in"},
		{"box_records", "Company in a Box records (coverage, drift, exceptions, checklists, acknowledgements, reviewed changes)", "", box},
	}
	var out []*Inventory
	for _, l := range list {
		inv := &Inventory{ID: l.id, Title: l.title, Status: NotRead, Reason: "needs " + l.needs, ReadAt: at, Summary: "not read",
			Queries: []string{"none sent: Microsoft Graph cannot read this with the audit's sign-in"}}
		if l.collector != "" {
			inv.Collectors = []string{l.collector}
		} else {
			inv.Collectors = []string{"coverage", "drift", "exceptions", "checklists", "acknowledgements", "pull_requests"}
			inv.Reason = l.needs
		}
		out = append(out, inv)
	}
	return out
}
