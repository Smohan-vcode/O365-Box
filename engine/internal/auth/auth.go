// Package auth signs in to Microsoft Entra ID for onboarding (SPEC §18.2).
// Tokens are kept in memory only (R12); nothing here writes to disk.
package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GraphCLIClientID is Microsoft's first-party "Microsoft Graph Command Line
// Tools" public client, accepted by every tenant (SPEC §3).
const GraphCLIClientID = "14d82eec-204b-4c2f-b7e8-296a70dab67e"

// Authority is the Entra ID sign-in host.
var Authority = "https://login.microsoftonline.com"

// Token is an access token held in memory.
type Token struct {
	Access    string
	ExpiresAt time.Time
	Claims    map[string]any
	// Refresh is the refresh token, returned when the sign-in asked for
	// offline_access. It stays in memory and is only ever sent back to the
	// token endpoint (Redeem); never logged or written (R12).
	Refresh string
}

// Drop clears the token's secrets.
func (t *Token) Drop() {
	if t != nil {
		t.Access, t.Refresh = "", ""
	}
}

// TenantID returns the token's tid claim.
func (t *Token) TenantID() string { s, _ := t.Claims["tid"].(string); return s }

// Subject returns the signed-in user's principal name or object id.
func (t *Token) Subject() string {
	for _, k := range []string{"upn", "preferred_username", "unique_name", "oid"} {
		if s, ok := t.Claims[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// Scopes returns the token's delegated scopes.
func (t *Token) Scopes() []string { s, _ := t.Claims["scp"].(string); return strings.Fields(s) }

// DeviceCode is what the user needs to complete a device-code sign-in.
type DeviceCode struct {
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Message         string `json:"message"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	deviceCode      string
}

// GraphScopes turns short names (User.Read.All) into Graph resource scopes.
func GraphScopes(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "https://graph.microsoft.com/" + n
	}
	return out
}

// SignInDeviceCode runs the device-code flow: onCode is called with the code
// to show, and the call returns when the user has signed in.
func SignInDeviceCode(ctx context.Context, hc *http.Client, tenant, clientID string, scopes []string, onCode func(DeviceCode)) (*Token, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	form := url.Values{"client_id": {clientID}, "scope": {strings.Join(scopes, " ")}}
	var dc struct {
		DeviceCode
		DeviceCodeRaw string `json:"device_code"`
	}
	if err := postForm(ctx, hc, Authority+"/"+url.PathEscape(tenant)+"/oauth2/v2.0/devicecode", form, &dc); err != nil {
		return nil, fmt.Errorf("start device sign-in: %w", err)
	}
	code := dc.DeviceCode
	code.deviceCode = dc.DeviceCodeRaw
	if onCode != nil {
		onCode(code)
	}
	interval := time.Duration(code.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(code.ExpiresIn) * time.Second)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		if time.Now().After(deadline) {
			return nil, errors.New("the sign-in code expired before sign-in finished")
		}
		var tr tokenResponse
		err := postForm(ctx, hc, Authority+"/"+url.PathEscape(tenant)+"/oauth2/v2.0/token", url.Values{
			"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "client_id": {clientID}, "device_code": {code.deviceCode},
		}, &tr)
		var oe *OAuthError
		if errors.As(err, &oe) {
			switch oe.Code {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 5 * time.Second
				continue
			}
			return nil, err
		}
		if err != nil {
			return nil, err
		}
		return tr.token()
	}
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func (t tokenResponse) token() (*Token, error) {
	claims, err := Claims(t.AccessToken)
	if err != nil {
		return nil, err
	}
	return &Token{Access: t.AccessToken, Refresh: t.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(t.ExpiresIn) * time.Second), Claims: claims}, nil
}

// Redeem exchanges the refresh token of a delegated sign-in for an access
// token to another resource (scopes such as
// "https://outlook.office365.com/.default"), for the same person and the
// same public client. Entra refuses when that client has no consent or
// pre-authorisation for the resource. The returned token carries the
// rotated refresh token. Nothing is stored.
func Redeem(ctx context.Context, hc *http.Client, tenant, clientID, refresh string, scopes []string) (*Token, error) {
	if refresh == "" {
		return nil, errors.New("the sign-in returned no refresh token (offline_access was not granted)")
	}
	if hc == nil {
		hc = http.DefaultClient
	}
	var tr tokenResponse
	err := postForm(ctx, hc, Authority+"/"+url.PathEscape(tenant)+"/oauth2/v2.0/token", url.Values{
		"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {refresh}, "scope": {strings.Join(scopes, " ")},
	}, &tr)
	if err != nil {
		return nil, err
	}
	t, err := tr.token()
	if err != nil {
		return nil, err
	}
	if t.Refresh == "" {
		t.Refresh = refresh
	}
	return t, nil
}

// OAuthError is an error response from the token endpoints.
type OAuthError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e *OAuthError) Error() string {
	d, _, _ := strings.Cut(e.Description, "\r\n")
	return e.Code + ": " + d
}

func postForm(ctx context.Context, hc *http.Client, u string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var oe OAuthError
		if json.NewDecoder(resp.Body).Decode(&oe) == nil && oe.Code != "" {
			return &oe
		}
		return fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Claims decodes a JWT's payload without verifying it: the token came
// straight from Entra over TLS and is only read for display and checks.
func Claims(jwt string) (map[string]any, error) {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return nil, errors.New("access token is not a JWT")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("access token payload: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("access token payload: %w", err)
	}
	return m, nil
}

// TenantFromDomain resolves a tenant ID from a domain or tenant ID through
// the public OpenID configuration (no sign-in needed).
func TenantFromDomain(ctx context.Context, hc *http.Client, domainOrID string) (string, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Authority+"/"+url.PathEscape(domainOrID)+"/v2.0/.well-known/openid-configuration", nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("no Microsoft 365 organisation found for %q", domainOrID)
	}
	var cfg struct {
		Issuer string `json:"issuer"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return "", err
	}
	// issuer: https://login.microsoftonline.com/<tenant-id>/v2.0
	p := strings.Split(strings.TrimSuffix(cfg.Issuer, "/v2.0"), "/")
	return p[len(p)-1], nil
}
