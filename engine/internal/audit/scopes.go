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
	Scopes       []string // granted (scp)
	WriteScopes  []string // granted scopes that are not read-only
	Roles        []string // directory role template ids (wids)
	GlobalReader bool
	WriterRole   bool // holds a role that can change the tenant
}

// NewSession reads the claims the audit relies on.
func NewSession(tenant, subject string, scopes []string, claims map[string]any) Session {
	s := Session{Tenant: tenant, Subject: subject, Scopes: scopes, WriteScopes: WriteScopes(scopes)}
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
