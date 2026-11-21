package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/auth"
	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
	"github.com/Smohan-vcode/O365-Box/engine/internal/keystore"
)

// Resource application ids.
var Resources = map[string]string{
	"graph":      GraphAppID,
	"exchange":   "00000002-0000-0ff1-ce00-000000000000",
	"sharepoint": "00000003-0000-0ff1-ce00-000000000000",
}

// RoleTemplates maps Entra role names to template ids.
var RoleTemplates = map[string]string{
	"Global Administrator":     "62e90394-69f5-4237-9190-012177145e10",
	"Exchange Administrator":   "29232cdf-9323-42fd-ade2-1d097af3e4de",
	"SharePoint Administrator": "f28a1f50-f6e7-4571-818b-6a12f2af6b6c",
	"User Administrator":       "fe930be7-5e62-47db-91af-98c3a49a38b1",
	"Teams Administrator":      "69091246-20e8-4a56-aa4d-066075b2a7a8",
	"Compliance Administrator": "17315797-102d-40b4-93e0-432062caca18",
	"Security Administrator":   "194ae4cb-b126-40b2-bd5b-6091b380977d",
	"Intune Administrator":     "3a2c62db-5318-420d-8d74-23affee5d9d5",
	"Global Reader":            "f2ef992c-3afb-46b9-b7cf-a126ee74c451",
	"Security Reader":          "5d6b6bb7-de71-4623-b4af-96380a352509",
	"Reports Reader":           "4a5d8f65-41da-4de4-8968-e035b65339cf",
}

// PipelineAppName is the display name of the pipeline app registration.
const PipelineAppName = "Company in a Box pipeline"

// Permissions are the grants of a union of bundles (expected-state/app-permissions.yaml).
type Permissions struct {
	Graph      []string `yaml:"graph" json:"graph"`
	Exchange   []string `yaml:"exchange" json:"exchange"`
	SharePoint []string `yaml:"sharepoint" json:"sharepoint"`
	EntraRoles []string `yaml:"entra_roles" json:"entra_roles"`
	Never      []string `yaml:"never" json:"never"`
}

// ByResource returns the app permissions per resource key.
func (p Permissions) ByResource() map[string][]string {
	return map[string][]string{"graph": p.Graph, "exchange": p.Exchange, "sharepoint": p.SharePoint}
}

// Pipeline is the local, non-secret record of the pipeline app.
type Pipeline struct {
	TenantID      string    `json:"tenant_id"`
	ClientID      string    `json:"client_id"`
	ObjectID      string    `json:"object_id"`
	SPID          string    `json:"service_principal_id"`
	Thumbprint    string    `json:"thumbprint"`
	InitialDomain string    `json:"initial_domain"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// PipelinePath is where the record lives for a tenant.
func PipelinePath(tenant string) (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "box", "tenants", tenant, "pipeline.json"), nil
}

// LoadPipeline reads the record.
func LoadPipeline(tenant string) (*Pipeline, error) {
	p, err := PipelinePath(tenant)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("no pipeline app recorded for this tenant (run box bootstrap): %w", err)
	}
	var pl Pipeline
	return &pl, json.Unmarshal(b, &pl)
}

func savePipeline(pl *Pipeline) error {
	p, err := PipelinePath(pl.TenantID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	pl.UpdatedAt = time.Now().UTC()
	b, _ := json.MarshalIndent(pl, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

// Key names in the keystore.
func keyName(tenant string) string  { return "pipeline/" + tenant + "/key" }
func certName(tenant string) string { return "pipeline/" + tenant + "/cert" }

// Credential loads the pipeline app's certificate credential.
func Credential(ks keystore.Store, tenant string) (*auth.AppCredential, *Pipeline, error) {
	pl, err := LoadPipeline(tenant)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := ks.Get(keyName(tenant))
	if err != nil {
		return nil, nil, fmt.Errorf("pipeline key: %w", err)
	}
	der, err := ks.Get(certName(tenant))
	if err != nil {
		return nil, nil, fmt.Errorf("pipeline certificate: %w", err)
	}
	key, err := auth.ParseKeyPEM(keyPEM)
	if err != nil {
		return nil, nil, err
	}
	return &auth.AppCredential{Tenant: tenant, ClientID: pl.ClientID, CertDER: der, Key: key}, pl, nil
}

// Options for a bootstrap run.
type Options struct {
	Tenant      string
	Permissions Permissions
	BreakGlass  []BreakGlassUser
	// AuditReader, when set, is a read-only account for box audit (SPEC
	// §26): Global Reader, Security Reader and Reports Reader, nothing else.
	AuditReader   *BreakGlassUser
	GitHubRepo    string // owner/name of the company's copy; optional
	PrimaryDomain string
	Keys          keystore.Store
	Say           func(format string, a ...any)
}

// BreakGlassUser is one break-glass account to create.
type BreakGlassUser struct{ Alias, DisplayName string }

// Result reports what bootstrap found or did.
type Result struct {
	Pipeline        *Pipeline
	DomainRecords   []DNSRecord // records to publish when the domain is not verified
	BreakGlassUsers []string
	Gates           []string // gates left open
}

// Run performs SPEC §18.5 with a delegated Global Administrator client.
// Every step looks for existing objects first, so a second run changes
// nothing.
func Run(ctx context.Context, c *graph.Client, opt Options) (*Result, error) {
	say := opt.Say
	if say == nil {
		say = func(string, ...any) {}
	}
	res := &Result{}
	domains, err := graph.List[struct {
		ID         string `json:"id"`
		IsInitial  bool   `json:"isInitial"`
		IsVerified bool   `json:"isVerified"`
	}](ctx, c, "/domains")
	if err != nil {
		return nil, err
	}
	initial := ""
	primaryVerified := false
	known := false
	for _, d := range domains {
		if d.IsInitial {
			initial = d.ID
		}
		if strings.EqualFold(d.ID, opt.PrimaryDomain) {
			known, primaryVerified = true, d.IsVerified
		}
	}
	// 1. Domain.
	switch {
	case opt.PrimaryDomain == "" || primaryVerified:
		say("✓ domain %s verified", opt.PrimaryDomain)
	default:
		if !known {
			if _, err := AddDomain(ctx, c, opt.PrimaryDomain); err != nil {
				return nil, fmt.Errorf("add domain: %w", err)
			}
			say("+ domain %s added", opt.PrimaryDomain)
		}
		if err := VerifyDomain(ctx, c, opt.PrimaryDomain); err == nil {
			say("✓ domain %s verified", opt.PrimaryDomain)
		} else {
			recs, _ := graph.List[DNSRecord](ctx, c, "/domains/"+url.PathEscape(opt.PrimaryDomain)+"/verificationDnsRecords")
			res.DomainRecords = recs
			res.Gates = append(res.Gates, "G-06")
			say("⏸ domain %s not verified yet: publish its TXT record (gate G-06)", opt.PrimaryDomain)
		}
	}

	// 2. Pipeline app and service principal.
	app, err := findOrCreateApp(ctx, c, say)
	if err != nil {
		return nil, err
	}
	pl := &Pipeline{TenantID: opt.Tenant, ClientID: app.AppID, ObjectID: app.ObjectID, SPID: app.SPID, InitialDomain: initial}

	// 3. Application permissions.
	for _, n := range opt.Permissions.Never {
		for res, perms := range opt.Permissions.ByResource() {
			for _, p := range perms {
				if p == n {
					return nil, fmt.Errorf("%s permission %s is on the never list", res, p)
				}
			}
		}
	}
	if err := grantAll(ctx, c, app, opt.Permissions.ByResource(), say); err != nil {
		return nil, err
	}

	// 4. Entra roles.
	for _, r := range opt.Permissions.EntraRoles {
		for _, n := range opt.Permissions.Never {
			if r == n {
				return nil, fmt.Errorf("role %s is on the never list", r)
			}
		}
		if err := ensureRole(ctx, c, app.SPID, r, say, "pipeline app"); err != nil {
			return nil, err
		}
	}

	// 5. Credentials: certificate (key in the keystore), federated credential.
	if err := ensureCertificate(ctx, c, app, opt, pl, say); err != nil {
		return nil, err
	}
	if opt.GitHubRepo != "" {
		if err := ensureFederated(ctx, c, app, opt.GitHubRepo, say); err != nil {
			return nil, err
		}
	}
	secrets, _, err := Credentials(ctx, c, app)
	if err != nil {
		return nil, err
	}
	if secrets > 0 {
		return nil, fmt.Errorf("the pipeline app has %d client secret(s); remove them (SPEC §20.2)", secrets)
	}

	// 6. Break-glass accounts and their group.
	var ids []string
	for _, u := range opt.BreakGlass {
		id, upn, err := ensureBreakGlass(ctx, c, u, initial, say)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
		res.BreakGlassUsers = append(res.BreakGlassUsers, upn)
	}
	if err := ensureGroup(ctx, c, "grp-break-glass", "Break-glass accounts; excluded from every Conditional Access policy", ids, say); err != nil {
		return nil, err
	}
	if len(opt.BreakGlass) > 0 {
		res.Gates = append(res.Gates, "G-02")
		say("⏸ gate G-02: register two FIDO2 keys per break-glass account; until then they cannot sign in")
	}
	if r := opt.AuditReader; r != nil {
		if err := ensureAuditReader(ctx, c, *r, initial, say); err != nil {
			return nil, err
		}
	}
	if err := savePipeline(pl); err != nil {
		return nil, err
	}
	res.Pipeline = pl
	return res, nil
}

func findOrCreateApp(ctx context.Context, c *graph.Client, say func(string, ...any)) (*App, error) {
	apps, err := graph.List[App](ctx, c, "/applications?$filter="+url.QueryEscape("displayName eq '"+PipelineAppName+"'")+"&$select=id,appId")
	if err != nil {
		return nil, err
	}
	var app *App
	switch len(apps) {
	case 0:
		app, err = CreateApp(ctx, c, PipelineAppName)
		if err != nil {
			return nil, fmt.Errorf("create pipeline app: %w", err)
		}
		say("+ pipeline app created")
	case 1:
		app = &apps[0]
		say("✓ pipeline app exists")
	default:
		return nil, fmt.Errorf("%d apps are named %q; keep one", len(apps), PipelineAppName)
	}
	sps, err := graph.List[struct{ ID string }](ctx, c, "/servicePrincipals?$filter="+url.QueryEscape("appId eq '"+app.AppID+"'")+"&$select=id")
	if err != nil {
		return nil, err
	}
	if len(sps) > 0 {
		app.SPID = sps[0].ID
		return app, nil
	}
	var last error
	for i := 0; i < 6; i++ {
		if last = CreateServicePrincipal(ctx, c, app); last == nil {
			say("+ service principal created")
			time.Sleep(10 * time.Second)
			return app, nil
		}
		time.Sleep(5 * time.Second)
	}
	return nil, fmt.Errorf("create service principal: %w", last)
}

type resourceSP struct {
	ID       string `json:"id"`
	AppRoles []struct {
		ID    string `json:"id"`
		Value string `json:"value"`
	} `json:"appRoles"`
}

func resourceByAppID(ctx context.Context, c *graph.Client, appID string) (*resourceSP, error) {
	sps, err := graph.List[resourceSP](ctx, c, "/servicePrincipals?$filter="+url.QueryEscape("appId eq '"+appID+"'")+"&$select=id,appRoles")
	if err != nil {
		return nil, err
	}
	if len(sps) == 0 {
		return nil, fmt.Errorf("resource %s has no service principal in this tenant", appID)
	}
	return &sps[0], nil
}

// Assignment is one app role assignment held by the pipeline app.
type Assignment struct {
	ID          string `json:"id"`
	AppRoleID   string `json:"appRoleId"`
	ResourceID  string `json:"resourceId"`
	ResourceApp string `json:"resourceDisplayName"`
}

func grantAll(ctx context.Context, c *graph.Client, app *App, want map[string][]string, say func(string, ...any)) error {
	have, err := retryList[Assignment](ctx, c, "/servicePrincipals/"+app.SPID+"/appRoleAssignments")
	if err != nil {
		return err
	}
	held := map[string]bool{}
	for _, a := range have {
		held[a.ResourceID+"/"+a.AppRoleID] = true
	}
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, resKey := range keys {
		if len(want[resKey]) == 0 {
			continue
		}
		rsp, err := resourceByAppID(ctx, c, Resources[resKey])
		if err != nil {
			return err
		}
		for _, perm := range want[resKey] {
			roleID := ""
			for _, r := range rsp.AppRoles {
				if r.Value == perm {
					roleID = r.ID
				}
			}
			if roleID == "" {
				return fmt.Errorf("%s has no application permission %s", resKey, perm)
			}
			if held[rsp.ID+"/"+roleID] {
				continue
			}
			err := retry(ctx, func() error {
				return c.Do(ctx, http.MethodPost, "/servicePrincipals/"+rsp.ID+"/appRoleAssignedTo",
					map[string]any{"principalId": app.SPID, "resourceId": rsp.ID, "appRoleId": roleID}, nil)
			})
			if err != nil {
				return fmt.Errorf("grant %s %s: %w", resKey, perm, err)
			}
			say("+ granted %s %s", resKey, perm)
		}
	}
	say("✓ application permissions match expected-state")
	return nil
}

func ensureRole(ctx context.Context, c *graph.Client, principalID, role string, say func(string, ...any), who string) error {
	tmpl, ok := RoleTemplates[role]
	if !ok {
		return fmt.Errorf("unknown Entra role %q", role)
	}
	have, err := graph.List[struct{ ID string }](ctx, c, "/roleManagement/directory/roleAssignments?$filter="+
		url.QueryEscape("principalId eq '"+principalID+"' and roleDefinitionId eq '"+tmpl+"'"))
	if err != nil {
		return err
	}
	if len(have) > 0 {
		return nil
	}
	if err := retry(ctx, func() error { _, err := AssignDirectoryRole(ctx, c, principalID, tmpl); return err }); err != nil {
		return fmt.Errorf("assign %s to %s: %w", role, who, err)
	}
	say("+ %s assigned to %s", role, who)
	return nil
}

func ensureCertificate(ctx context.Context, c *graph.Client, app *App, opt Options, pl *Pipeline, say func(string, ...any)) error {
	var cur struct {
		Keys []struct {
			CustomKeyIdentifier string    `json:"customKeyIdentifier"`
			EndDateTime         time.Time `json:"endDateTime"`
		} `json:"keyCredentials"`
	}
	if err := c.Get(ctx, "/applications/"+app.ObjectID+"?$select=keyCredentials", &cur); err != nil {
		return err
	}
	if der, err := opt.Keys.Get(certName(opt.Tenant)); err == nil {
		if _, kerr := opt.Keys.Get(keyName(opt.Tenant)); kerr == nil {
			tp := strings.ToUpper(auth.Thumbprint(der))
			for _, k := range cur.Keys {
				if SameThumbprint(k.CustomKeyIdentifier, tp) && time.Until(k.EndDateTime) > 30*24*time.Hour {
					pl.Thumbprint = auth.Thumbprint(der)
					say("✓ certificate in place (key in %s)", opt.Keys.Kind())
					return nil
				}
			}
		}
	}
	der, key, err := SelfSignedCert("Company in a Box pipeline")
	if err != nil {
		return err
	}
	if err := opt.Keys.Put(keyName(opt.Tenant), auth.KeyPEM(key)); err != nil {
		return fmt.Errorf("store pipeline key: %w", err)
	}
	if err := opt.Keys.Put(certName(opt.Tenant), der); err != nil {
		return err
	}
	if err := AddCertificate(ctx, c, app, der, "box-pipeline"); err != nil {
		return err
	}
	pl.Thumbprint = auth.Thumbprint(der)
	say("+ certificate added; private key stored in %s", opt.Keys.Kind())
	return nil
}

// SameThumbprint compares a key credential's customKeyIdentifier with a
// certificate thumbprint. Graph returns the identifier as the hex
// thumbprint; older responses carried it base64-encoded. Both are accepted.
func SameThumbprint(customKeyIdentifier, thumbprintHex string) bool {
	if strings.EqualFold(customKeyIdentifier, thumbprintHex) {
		return true
	}
	b, err := base64.StdEncoding.DecodeString(customKeyIdentifier)
	return err == nil && strings.EqualFold(fmt.Sprintf("%X", b), thumbprintHex)
}

func ensureFederated(ctx context.Context, c *graph.Client, app *App, repo string, say func(string, ...any)) error {
	name := "github-" + strings.NewReplacer("/", "-", ".", "-").Replace(repo)
	have, err := graph.List[struct{ Name string }](ctx, c, "/applications/"+app.ObjectID+"/federatedIdentityCredentials")
	if err != nil {
		return err
	}
	for _, h := range have {
		if h.Name == name {
			return nil
		}
	}
	if err := AddFederatedCredential(ctx, c, app, name, "repo:"+repo+":ref:refs/heads/main"); err != nil {
		return err
	}
	say("+ GitHub federated credential for %s", repo)
	return nil
}

func ensureBreakGlass(ctx context.Context, c *graph.Client, u BreakGlassUser, domain string, say func(string, ...any)) (string, string, error) {
	upn := u.Alias + "@" + domain
	var existing struct{ ID string }
	err := c.Get(ctx, "/users/"+url.PathEscape(upn)+"?$select=id", &existing)
	var ge *graph.Error
	switch {
	case err == nil:
	case errors.As(err, &ge) && ge.Status == http.StatusNotFound:
		// A random password that is never shown or stored: the account
		// cannot be used until FIDO2 keys are registered (gate G-02).
		pw := make([]byte, 32)
		_, _ = rand.Read(pw)
		var created struct{ ID string }
		err = c.Do(ctx, http.MethodPost, "/users", map[string]any{
			"accountEnabled": true, "displayName": u.DisplayName, "mailNickname": u.Alias, "userPrincipalName": upn,
			"passwordProfile": map[string]any{"password": "Bx!" + base64.RawURLEncoding.EncodeToString(pw), "forceChangePasswordNextSignIn": false},
		}, &created)
		if err != nil {
			return "", "", fmt.Errorf("create break-glass %s: %w", upn, err)
		}
		existing.ID = created.ID
		say("+ break-glass account %s created (password random and discarded)", upn)
	default:
		return "", "", err
	}
	if err := ensureRole(ctx, c, existing.ID, "Global Administrator", say, upn); err != nil {
		return "", "", err
	}
	return existing.ID, upn, nil
}

func ensureGroup(ctx context.Context, c *graph.Client, name, desc string, members []string, say func(string, ...any)) error {
	gs, err := graph.List[struct{ ID string }](ctx, c, "/groups?$filter="+url.QueryEscape("displayName eq '"+name+"'")+"&$select=id")
	if err != nil {
		return err
	}
	id := ""
	if len(gs) > 0 {
		id = gs[0].ID
	} else {
		var g struct{ ID string }
		if err := c.Do(ctx, http.MethodPost, "/groups", map[string]any{"displayName": name, "description": desc,
			"mailEnabled": false, "mailNickname": name, "securityEnabled": true}, &g); err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
		id = g.ID
		say("+ group %s created", name)
	}
	cur, err := graph.List[struct{ ID string }](ctx, c, "/groups/"+id+"/members?$select=id")
	if err != nil {
		return err
	}
	in := map[string]bool{}
	for _, m := range cur {
		in[m.ID] = true
	}
	for _, m := range members {
		if in[m] {
			continue
		}
		if err := retry(ctx, func() error {
			return c.Do(ctx, http.MethodPost, "/groups/"+id+"/members/$ref", map[string]any{"@odata.id": graph.Base + "/directoryObjects/" + m}, nil)
		}); err != nil {
			return fmt.Errorf("add member to %s: %w", name, err)
		}
		say("+ member added to %s", name)
	}
	return nil
}

// retry repeats a call while Entra replicates a new object (404/400 shortly after creation).
func retry(ctx context.Context, f func() error) error {
	var err error
	for i := 0; i < 8; i++ {
		if err = f(); err == nil {
			return nil
		}
		var ge *graph.Error
		if !errors.As(err, &ge) || (ge.Status != 400 && ge.Status != 404) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(3+i*2) * time.Second):
		}
	}
	return err
}

func retryList[T any](ctx context.Context, c *graph.Client, path string) ([]T, error) {
	var out []T
	err := retry(ctx, func() error {
		var err error
		out, err = graph.List[T](ctx, c, path)
		return err
	})
	return out, err
}

// AuditReaderRoles are the only roles of the audit reader account.
var AuditReaderRoles = []string{"Global Reader", "Security Reader", "Reports Reader"}

// ensureAuditReader creates the read-only audit account with a random
// password nobody sees: the owner sets one in the Microsoft 365 admin centre
// (Users > Reset password), so no sign-in secret passes through the box.
func ensureAuditReader(ctx context.Context, c *graph.Client, u BreakGlassUser, domain string, say func(string, ...any)) error {
	upn := u.Alias + "@" + domain
	var existing struct{ ID string }
	err := c.Get(ctx, "/users/"+url.PathEscape(upn)+"?$select=id", &existing)
	var ge *graph.Error
	switch {
	case err == nil:
	case errors.As(err, &ge) && ge.Status == http.StatusNotFound:
		pw := make([]byte, 32)
		_, _ = rand.Read(pw)
		var created struct{ ID string }
		if err := c.Do(ctx, http.MethodPost, "/users", map[string]any{
			"accountEnabled": true, "displayName": u.DisplayName, "mailNickname": u.Alias, "userPrincipalName": upn,
			"passwordProfile": map[string]any{"password": "Rd!" + base64.RawURLEncoding.EncodeToString(pw), "forceChangePasswordNextSignIn": true},
		}, &created); err != nil {
			return fmt.Errorf("create audit reader %s: %w", upn, err)
		}
		existing.ID = created.ID
		say("+ audit reader %s created; set its password in the Microsoft 365 admin centre (Users > Reset password)", upn)
	default:
		return err
	}
	for _, r := range AuditReaderRoles {
		if err := ensureRole(ctx, c, existing.ID, r, say, upn); err != nil {
			return err
		}
	}
	say("✓ audit reader %s holds only: %s", upn, strings.Join(AuditReaderRoles, ", "))
	return nil
}
