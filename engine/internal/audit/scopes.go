// Package audit builds the read-only audit pack (SPEC §26, SC-26): a
// delegated read-only sign-in, inventories read over Microsoft Graph, the
// catalogue settings read through the policy steps' verify, a status per
// framework control, and the pack (index.html, controls.csv, evidence/,
// manifest.sha256, gaps.md) written to a folder the person chooses.
package audit

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ScopesFile is the audit sign-in's scope list, relative to the repository.
const ScopesFile = "expected-state/audit-scopes.yaml"

// Scopes is expected-state/audit-scopes.yaml.
type Scopes struct {
	Client    string   `yaml:"client"`
	Delegated []string `yaml:"delegated"`
	// Resources are the workloads read with their own delegated token
	// (T16-06); see tokens.go.
	Resources []Resource `yaml:"resources"`
}

// LoadScopes reads the scope file and refuses any scope that is not
// read-only.
func LoadScopes(path string) (*Scopes, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Scopes
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := CheckScopes(s.Delegated); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := CheckResources(s.Resources); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

// readScope is "<Resource>.Read" or "<Resource>.ReadBasic", with an
// optional qualifier ("All", "Directory", "ConditionalAccess"). The verb is
// the second segment: ReadWrite, Send, Manage*, FullControl, AccessAsUser
// and every other verb fail.
var readScope = regexp.MustCompile(`^[A-Za-z]+\.(Read|ReadBasic)(\.[A-Za-z]+)?$`)

// ReadOnlyScope reports whether a Graph scope name can only read.
func ReadOnlyScope(s string) bool { return readScope.MatchString(s) }

// CheckScopes fails when the list is empty, repeats a scope or holds one
// that is not read-only.
func CheckScopes(list []string) error {
	if len(list) == 0 {
		return fmt.Errorf("no scopes listed")
	}
	seen := map[string]bool{}
	var bad []string
	for _, s := range list {
		if seen[s] {
			return fmt.Errorf("scope %s is listed twice", s)
		}
		seen[s] = true
		if !ReadOnlyScope(s) {
			bad = append(bad, s)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("scope(s) that are not read-only: %s", strings.Join(bad, ", "))
	}
	return nil
}

var oidcScopes = map[string]bool{"openid": true, "profile": true, "email": true, "offline_access": true}

// WriteScopes lists the scopes of a token (its scp claim) that are not
// read-only. A token from a public client carries every scope ever
// consented for that client, so it can hold more than the audit asked for.
func WriteScopes(granted []string) []string {
	var out []string
	for _, s := range granted {
		if !oidcScopes[s] && !ReadOnlyScope(s) {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// Directory role template ids (fixed by Microsoft, the same in every
// tenant) of roles that can only read.
const (
	RoleGlobalReader = "f2ef992c-3afb-46b9-b7cf-a126ee74c451"
	RoleGlobalAdmin  = "62e90394-69f5-4237-9190-012177145e10"
)

var readerRoles = map[string]string{
	RoleGlobalReader:                       "Global Reader",
	"5d6b6bb7-de71-4623-b4af-96380a352509": "Security Reader",
	"4a5d8f65-41da-4de4-8968-e035b65339cf": "Reports Reader",
	"88d8e3e3-8f55-4a1e-953a-9b9898b8876b": "Directory Readers",
	"790c1fb9-7f7d-4f88-86a1-ef1f95c05c1b": "Message Center Reader",
	"75934031-6c7e-415a-99d7-48dbd49e875e": "Usage Summary Reports Reader",
	"b79fbf4d-3ef9-4689-8143-76b194e85509": "User", // every member's default role
}

// Session describes what the sign-in can do, from the token's claims.
type Session struct {
	Tenant       string
	Subject      string
	Object       string   // the person's object id (oid)
	Scopes       []string // granted (scp)
	WriteScopes  []string // granted scopes that are not read-only
	Roles        []string // directory role template ids (wids)
	GlobalReader bool
	WriterRole   bool // holds a role that can change the tenant
}

// NewSession reads the claims the audit relies on.
func NewSession(tenant, subject string, scopes []string, claims map[string]any) Session {
	s := Session{Tenant: tenant, Subject: subject, Scopes: scopes, WriteScopes: WriteScopes(scopes)}
	s.Object, _ = claims["oid"].(string)
	if w, ok := claims["wids"].([]any); ok {
		for _, v := range w {
			id, _ := v.(string)
			s.Roles = append(s.Roles, id)
			if id == RoleGlobalReader {
				s.GlobalReader = true
			}
			if _, reader := readerRoles[id]; !reader {
				s.WriterRole = true
			}
		}
	}
	return s
}

// ProvidersAllowed reports whether provider workers may get the token: only
// when the token cannot write, by its scopes or by the person's roles.
// Otherwise the settings they read are reported "not read" with the reason.
func (s Session) ProvidersAllowed() (bool, string) {
	if len(s.WriteScopes) == 0 {
		return true, ""
	}
	if !s.WriterRole && len(s.Roles) > 0 {
		return true, ""
	}
	return false, fmt.Sprintf("the sign-in token carries write scopes consented earlier for the client (%s) and the person holds a role that can change the tenant: sign in as Global Reader to read these settings",
		strings.Join(s.WriteScopes, ", "))
}

// ResourcesAllowed reports whether the audit may ask for, and hand provider
// workers, tokens for Exchange Online, Security & Compliance, SharePoint and
// Teams. Their scopes (Exchange.Manage, AllSites.FullControl,
// user_impersonation) do not limit them to reads, so the person's roles must:
// every directory role the sign-in holds can only read, and it holds at
// least one (an empty wids claim proves nothing).
func (s Session) ResourcesAllowed() (bool, string) {
	switch {
	case s.WriterRole:
		return false, "the person who signed in holds a role that can change the tenant (" + RoleNames(s.Roles) +
			"); Exchange, Purview, SharePoint and Teams tokens are only requested for a reader-only account"
	case len(s.Roles) == 0:
		return false, "the sign-in token lists no directory role, so the audit cannot show the account only reads; sign in as Global Reader"
	}
	return true, ""
}

// SameReader checks that a token for another resource was issued to the
// person of this session, in the same tenant, and holds no role that can
// write. A device-code page can be completed by any account, so every token
// is checked, not only the first.
func (s Session) SameReader(claims map[string]any) error {
	tid, _ := claims["tid"].(string)
	oid, _ := claims["oid"].(string)
	if tid == "" || !strings.EqualFold(tid, s.Tenant) {
		return fmt.Errorf("the token is for tenant %q, not the audited tenant %s", tid, s.Tenant)
	}
	if s.Object == "" || !strings.EqualFold(oid, s.Object) {
		other := NewSession(tid, "", nil, claims)
		who := other.Object
		for _, k := range []string{"upn", "preferred_username", "unique_name"} {
			if v, _ := claims[k].(string); v != "" {
				who = v
				break
			}
		}
		return fmt.Errorf("the token was issued to another account (%s), not the one that signed in first; sign in as the same reader", who)
	}
	if other := NewSession(tid, "", nil, claims); other.WriterRole {
		return fmt.Errorf("the token holds a role that can change the tenant (%s)", RoleNames(other.Roles))
	}
	return nil
}

// roleNames are display names of common directory roles, for messages.
var roleNames = map[string]string{RoleGlobalAdmin: "Global Administrator",
	"29232cdf-9323-42fd-ade2-1d097af3e4de": "Exchange Administrator", "f28a1f50-f6e7-4571-818b-6a12f2af6b6c": "SharePoint Administrator",
	"fe930be7-5e62-47db-91af-98c3a49a38b1": "User Administrator", "69091246-20e8-4a56-aa4d-066075b2a7a8": "Teams Administrator",
	"194ae4cb-b126-40b2-bd5b-6091b380977d": "Security Administrator", "17315797-102d-40b4-93e0-432062caca18": "Compliance Administrator",
	"3a2c62db-5318-420d-8d74-23affee5d9d5": "Intune Administrator"}

// RoleNames lists the roles of a sign-in by name ("none" when it holds none
// beyond the default user role).
func RoleNames(ids []string) string {
	var out []string
	for _, id := range ids {
		if n, ok := readerRoles[id]; ok {
			if n != "User" {
				out = append(out, n)
			}
		} else if n, ok := roleNames[id]; ok {
			out = append(out, n)
		} else {
			out = append(out, "role "+id)
		}
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}
