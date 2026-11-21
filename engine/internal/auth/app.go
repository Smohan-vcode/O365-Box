package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// AppCredential signs in as the pipeline app with its certificate (client
// assertion). No client secret exists (SPEC §18.5).
type AppCredential struct {
	Tenant   string
	ClientID string
	CertDER  []byte
	Key      *rsa.PrivateKey
	HTTP     *http.Client

	mu    sync.Mutex
	cache map[string]*Token
}

// ParseKeyPEM reads a PKCS#8 or PKCS#1 RSA private key.
func ParseKeyPEM(b []byte) (*rsa.PrivateKey, error) {
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, errors.New("no PEM block")
	}
	if k, err := x509.ParsePKCS1PrivateKey(blk.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("not an RSA key")
	}
	return rk, nil
}

// KeyPEM encodes a private key as PKCS#8 PEM.
func KeyPEM(k *rsa.PrivateKey) []byte {
	b, _ := x509.MarshalPKCS8PrivateKey(k)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b})
}

// Thumbprint is the certificate's SHA-1 thumbprint in hex (as Entra shows it).
func Thumbprint(der []byte) string {
	h := sha1.Sum(der)
	return hex.EncodeToString(h[:])
}

// Token returns an app-only token for a resource, e.g.
// "https://graph.microsoft.com" or "https://outlook.office365.com".
func (a *AppCredential) Token(ctx context.Context, resource string) (*Token, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if t := a.cache[resource]; t != nil && time.Until(t.ExpiresAt) > 5*time.Minute {
		return t, nil
	}
	assertion, err := a.assertion()
	if err != nil {
		return nil, err
	}
	hc := a.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	var tr tokenResponse
	err = postForm(ctx, hc, Authority+"/"+url.PathEscape(a.Tenant)+"/oauth2/v2.0/token", url.Values{
		"grant_type": {"client_credentials"}, "client_id": {a.ClientID}, "scope": {resource + "/.default"},
		"client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"}, "client_assertion": {assertion},
	}, &tr)
	if err != nil {
		return nil, err
	}
	t, err := tr.token()
	if err != nil {
		return nil, err
	}
	if a.cache == nil {
		a.cache = map[string]*Token{}
	}
	a.cache[resource] = t
	return t, nil
}

func (a *AppCredential) assertion() (string, error) {
	x5t := sha1.Sum(a.CertDER)
	head, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "x5t": base64.RawURLEncoding.EncodeToString(x5t[:])})
	jti := make([]byte, 16)
	_, _ = rand.Read(jti)
	now := time.Now()
	body, _ := json.Marshal(map[string]any{
		"aud": Authority + "/" + a.Tenant + "/oauth2/v2.0/token", "iss": a.ClientID, "sub": a.ClientID,
		"jti": hex.EncodeToString(jti), "nbf": now.Add(-time.Minute).Unix(), "exp": now.Add(10 * time.Minute).Unix(),
	})
	signing := base64.RawURLEncoding.EncodeToString(head) + "." + base64.RawURLEncoding.EncodeToString(body)
	h := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.Key, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
