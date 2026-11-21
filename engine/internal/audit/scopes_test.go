package audit

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const repoRoot = "../../.."

// TestAuditScopesAreReadOnly is T16-01's exit criterion: the sign-in
// requests no write scope.
func TestAuditScopesAreReadOnly(t *testing.T) {
	s, err := LoadScopes(filepath.Join(repoRoot, ScopesFile))
	if err != nil {
		t.Fatal(err)
	}
	if s.Client != "microsoft-graph-command-line-tools" || len(s.Delegated) < 10 {
		t.Fatalf("scopes = %+v", s)
	}
	forbidden := regexp.MustCompile(`(?i)write|send|manage|fullcontrol|accessasuser|create|delete|invite|privileged`)
	for _, sc := range s.Delegated {
		if forbidden.MatchString(strings.SplitN(sc, ".", 2)[1]) {
			t.Errorf("scope %s can write", sc)
		}
		if !strings.Contains(sc, ".Read") {
			t.Errorf("scope %s is not a read scope", sc)
		}
	}
	for _, need := range []string{"Directory.Read.All", "Policy.Read.All", "AuditLog.Read.All", "Reports.Read.All", "RoleManagement.Read.Directory",
		"DeviceManagementManagedDevices.Read.All", "SecurityAlert.Read.All", "UserAuthenticationMethod.Read.All"} {
		found := false
		for _, sc := range s.Delegated {
			found = found || sc == need
		}
		if !found {
			t.Errorf("scope %s missing", need)
		}
	}
}

func TestReadOnlyScope(t *testing.T) {
	for sc, want := range map[string]bool{
		"User.Read.All": true, "RoleManagement.Read.Directory": true, "InformationProtectionPolicy.Read": true,
		"DeviceManagementManagedDevices.Read.All": true, "User.ReadBasic.All": true, "Policy.Read.ConditionalAccess": true,
		"User.ReadWrite.All": false, "Mail.Send": false, "Sites.FullControl.All": false, "Directory.AccessAsUser.All": false,
		"Exchange.ManageAsApp": false, "User.Invite.All": false, "Sites.Selected": false, "DeviceManagementManagedDevices.PrivilegedOperations.All": false,
		"Policy.ReadWrite.ConditionalAccess": false, "": false, "Read": false,
	} {
		if got := ReadOnlyScope(sc); got != want {
			t.Errorf("ReadOnlyScope(%q) = %v, want %v", sc, got, want)
		}
	}
	if err := CheckScopes([]string{"User.Read.All", "Mail.Send"}); err == nil || !strings.Contains(err.Error(), "Mail.Send") {
		t.Errorf("CheckScopes accepted a send scope: %v", err)
	}
	if err := CheckScopes([]string{"User.Read.All", "User.Read.All"}); err == nil {
		t.Error("CheckScopes accepted a repeated scope")
	}
}

func TestProvidersOnlyGetAReadOnlyToken(t *testing.T) {
	reader := map[string]any{"wids": []any{RoleGlobalReader, "b79fbf4d-3ef9-4689-8143-76b194e85509"}}
	admin := map[string]any{"wids": []any{RoleGlobalAdmin}}
	ro := []string{"User.Read.All", "openid", "profile"}
	rw := []string{"User.Read.All", "User.ReadWrite.All"}
	cases := []struct {
		scopes []string
		claims map[string]any
		ok     bool
	}{
		{ro, admin, true},  // the token cannot write
		{rw, reader, true}, // the person cannot write
		{rw, admin, false}, // both could: providers get nothing
		{rw, map[string]any{}, false},
	}
	for i, c := range cases {
		s := NewSession("t", "p", c.scopes, c.claims)
		if ok, why := s.ProvidersAllowed(); ok != c.ok {
			t.Errorf("case %d: allowed = %v (%s), want %v", i, ok, why, c.ok)
		}
	}
	if s := NewSession("t", "p", rw, reader); !s.GlobalReader || s.WriterRole || len(s.WriteScopes) != 1 {
		t.Errorf("session = %+v", s)
	}
}
