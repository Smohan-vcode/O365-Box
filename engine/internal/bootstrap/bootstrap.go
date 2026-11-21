// Package bootstrap holds the Graph actions of SPEC §18.5: the pipeline
// app, its permissions and credentials, Entra roles and domains. Spike S-06
// exercises each; T0-04 composes them into the bootstrap step group.
package bootstrap

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
)

// GraphAppID is Microsoft Graph's application id.
const GraphAppID = "00000003-0000-0000-c000-000000000000"

// App is a created application and its service principal.
type App struct {
	ObjectID string `json:"id"`
	AppID    string `json:"appId"`
	SPID     string `json:"-"`
}

// CreateApp creates a single-tenant application registration.
func CreateApp(ctx context.Context, c *graph.Client, name string) (*App, error) {
	var a App
	err := c.Do(ctx, http.MethodPost, "/applications", map[string]any{"displayName": name, "signInAudience": "AzureADMyOrg"}, &a)
	return &a, err
}

// CreateServicePrincipal creates the app's service principal.
func CreateServicePrincipal(ctx context.Context, c *graph.Client, a *App) error {
	var sp struct{ ID string }
	if err := c.Do(ctx, http.MethodPost, "/servicePrincipals", map[string]any{"appId": a.AppID}, &sp); err != nil {
		return err
	}
	a.SPID = sp.ID
	return nil
}

// GrantAppRole grants a Graph application permission (admin consent by app
// role assignment). It returns the assignment id.
func GrantAppRole(ctx context.Context, c *graph.Client, a *App, permission string) (string, error) {
	var sps struct {
		Value []struct {
			ID       string `json:"id"`
			AppRoles []struct {
				ID    string `json:"id"`
				Value string `json:"value"`
			} `json:"appRoles"`
		} `json:"value"`
	}
	if err := c.Get(ctx, "/servicePrincipals?$filter="+url.QueryEscape("appId eq '"+GraphAppID+"'")+"&$select=id,appRoles", &sps); err != nil {
		return "", err
	}
	if len(sps.Value) == 0 {
		return "", fmt.Errorf("Microsoft Graph service principal not found")
	}
	g := sps.Value[0]
	roleID := ""
	for _, r := range g.AppRoles {
		if r.Value == permission {
			roleID = r.ID
		}
	}
	if roleID == "" {
		return "", fmt.Errorf("Graph has no application permission %s", permission)
	}
	var out struct{ ID string }
	err := c.Do(ctx, http.MethodPost, "/servicePrincipals/"+g.ID+"/appRoleAssignedTo",
		map[string]any{"principalId": a.SPID, "resourceId": g.ID, "appRoleId": roleID}, &out)
	return out.ID, err
}

// AssignDirectoryRole assigns an Entra role by its template id at tenant
// scope. It returns the assignment id.
func AssignDirectoryRole(ctx context.Context, c *graph.Client, principalID, roleTemplateID string) (string, error) {
	var out struct{ ID string }
	err := c.Do(ctx, http.MethodPost, "/roleManagement/directory/roleAssignments",
		map[string]any{"principalId": principalID, "roleDefinitionId": roleTemplateID, "directoryScopeId": "/"}, &out)
	return out.ID, err
}

// AddFederatedCredential trusts GitHub Actions tokens for one subject.
func AddFederatedCredential(ctx context.Context, c *graph.Client, a *App, name, subject string) error {
	return c.Do(ctx, http.MethodPost, "/applications/"+a.ObjectID+"/federatedIdentityCredentials", map[string]any{
		"name": name, "issuer": "https://token.actions.githubusercontent.com", "subject": subject,
		"audiences": []string{"api://AzureADTokenExchange"}}, nil)
}

// AddCertificate adds a public certificate. The caller keeps the private
// key (in the OS keychain for real bootstraps; discarded in the spike).
func AddCertificate(ctx context.Context, c *graph.Client, a *App, certDER []byte, name string) error {
	return c.Do(ctx, http.MethodPatch, "/applications/"+a.ObjectID, map[string]any{"keyCredentials": []map[string]any{{
		"type": "AsymmetricX509Cert", "usage": "Verify", "key": base64.StdEncoding.EncodeToString(certDER), "displayName": name}}}, nil)
}

// SelfSignedCert makes an RSA 2048 certificate valid for a year.
func SelfSignedCert(cn string) (der []byte, key *rsa.PrivateKey, err error) {
	key, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature}
	der, err = x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	return der, key, err
}

// Credentials reports how many secrets and certificates the app has.
func Credentials(ctx context.Context, c *graph.Client, a *App) (secrets, certs int, err error) {
	var app struct {
		Password []any `json:"passwordCredentials"`
		Key      []any `json:"keyCredentials"`
	}
	if err := c.Get(ctx, "/applications/"+a.ObjectID+"?$select=passwordCredentials,keyCredentials", &app); err != nil {
		return 0, 0, err
	}
	return len(app.Password), len(app.Key), nil
}

// DNSRecord is a domain verification record.
type DNSRecord struct {
	RecordType string `json:"recordType"`
	Label      string `json:"label"`
	Text       string `json:"text"`
	MailExch   string `json:"mailExchange"`
	TTL        int    `json:"ttl"`
}

// AddDomain adds a domain (unverified) and returns its verification records.
func AddDomain(ctx context.Context, c *graph.Client, name string) ([]DNSRecord, error) {
	if err := c.Do(ctx, http.MethodPost, "/domains", map[string]any{"id": name}, nil); err != nil {
		return nil, err
	}
	return graph.List[DNSRecord](ctx, c, "/domains/"+url.PathEscape(name)+"/verificationDnsRecords")
}

// VerifyDomain asks Entra to check the verification record.
func VerifyDomain(ctx context.Context, c *graph.Client, name string) error {
	return c.Do(ctx, http.MethodPost, "/domains/"+url.PathEscape(name)+"/verify", nil, nil)
}

// DeleteDomain removes a domain.
func DeleteDomain(ctx context.Context, c *graph.Client, name string) error {
	return c.Do(ctx, http.MethodDelete, "/domains/"+url.PathEscape(name), nil, nil)
}

// DeleteApp deletes the application and purges it from the recycle bin.
func DeleteApp(ctx context.Context, c *graph.Client, a *App) error {
	if err := c.Do(ctx, http.MethodDelete, "/applications/"+a.ObjectID, nil, nil); err != nil {
		return err
	}
	return c.Do(ctx, http.MethodDelete, "/directory/deletedItems/"+a.ObjectID, nil, nil)
}
