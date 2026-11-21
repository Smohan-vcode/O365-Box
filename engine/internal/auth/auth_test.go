package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func jwt(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
}

// Redeem sends the refresh token to the token endpoint only, for the named
// client and scopes, and keeps the rotated refresh token.
func TestRedeem(t *testing.T) {
	var form map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = map[string]string{"path": r.URL.Path}
		for k := range r.PostForm {
			form[k] = r.PostForm.Get(k)
		}
		if form["refresh_token"] == "refused" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"AADSTS65001: consent required\r\nTrace ID: x"}`))
			return
		}
		rt := "rotated"
		if form["refresh_token"] == "keep" {
			rt = ""
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": jwt(map[string]any{"tid": "t1", "aud": "https://outlook.office365.com"}), "refresh_token": rt, "expires_in": 3600})
	}))
	defer srv.Close()
	old := Authority
	Authority = srv.URL
	defer func() { Authority = old }()

	tok, err := Redeem(context.Background(), srv.Client(), "t1", "client-1", "rt-1", []string{"https://outlook.office365.com/.default", "offline_access"})
	if err != nil {
		t.Fatal(err)
	}
	if form["path"] != "/t1/oauth2/v2.0/token" || form["grant_type"] != "refresh_token" || form["client_id"] != "client-1" || form["refresh_token"] != "rt-1" ||
		form["scope"] != "https://outlook.office365.com/.default offline_access" {
		t.Errorf("form = %v", form)
	}
	if tok.Refresh != "rotated" || tok.Claims["aud"] != "https://outlook.office365.com" || tok.ExpiresAt.IsZero() {
		t.Errorf("token = %+v", tok)
	}
	tok.Drop()
	if tok.Access != "" || tok.Refresh != "" {
		t.Error("Drop left a secret")
	}
	if tok, err := Redeem(context.Background(), srv.Client(), "t1", "c", "keep", []string{"x/.default"}); err != nil || tok.Refresh != "keep" {
		t.Errorf("no rotation: %v %+v", err, tok)
	}
	_, err = Redeem(context.Background(), srv.Client(), "t1", "c", "refused", []string{"x/.default"})
	var oe *OAuthError
	if !errors.As(err, &oe) || oe.Code != "invalid_grant" || strings.Contains(err.Error(), "Trace") {
		t.Errorf("refusal = %v", err)
	}
	if _, err := Redeem(context.Background(), srv.Client(), "t1", "c", "", nil); err == nil {
		t.Error("empty refresh token accepted")
	}
}
