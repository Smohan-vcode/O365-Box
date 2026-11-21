// Package audittest serves recorded Microsoft Graph responses for a
// fictional tenant (testdata/graph.json) to the audit's tests, and makes
// unsigned test tokens. Test use only.
package audittest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Tenant is the fictional tenant's id in the recorded responses.
const Tenant = "00000000-0000-4000-8000-00000000f00d"

// Graph is a fake Graph endpoint.
type Graph struct {
	Server *httptest.Server
	mu     sync.Mutex
	// Requests are "METHOD path?query" in order.
	Requests []string
}

// V1 and Beta are the bases to give the reader.
func (g *Graph) V1() string   { return g.Server.URL + "/v1.0" }
func (g *Graph) Beta() string { return g.Server.URL + "/beta" }

// Methods lists the distinct request methods seen.
func (g *Graph) Methods() map[string]int {
	g.mu.Lock()
	defer g.mu.Unlock()
	m := map[string]int{}
	for _, r := range g.Requests {
		meth, _, _ := strings.Cut(r, " ")
		m[meth]++
	}
	return m
}

// NewGraph starts the fake over TLS (the Graph client only sends https).
func NewGraph(t testing.TB) *Graph {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "testdata", "graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	g := &Graph{}
	var fixtures map[string]json.RawMessage
	g.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.Requests = append(g.Requests, r.Method+" "+r.URL.RequestURI())
		g.mu.Unlock()
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":{"code":"MethodNotAllowed","message":"the fake is read-only"}}`, http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, `{"error":{"code":"InvalidAuthenticationToken","message":"no token"}}`, http.StatusUnauthorized)
			return
		}
		key := r.URL.Path
		switch {
		case key == "/v1.0/users" && strings.Contains(r.URL.RawQuery, "Guest"):
			key += "?guest"
		case r.URL.Query().Get("page") != "":
			key += "?page=" + r.URL.Query().Get("page")
		}
		raw, ok := fixtures[key]
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"ResourceNotFound","message":"not recorded: ` + key + `"}}`))
			return
		}
		var rec struct {
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		}
		_ = json.Unmarshal(raw, &rec)
		if rec.Status == 0 {
			rec.Status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rec.Status)
		_, _ = w.Write(rec.Body)
	}))
	t.Cleanup(g.Server.Close)
	text := strings.ReplaceAll(string(b), "{{base}}", g.Server.URL)
	if err := json.Unmarshal([]byte(text), &fixtures); err != nil {
		t.Fatal(err)
	}
	return g
}

// Token makes an unsigned access token with the given claims (tests only;
// the audit reads claims and never verifies the signature itself).
func Token(claims map[string]any) string {
	enc := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	return enc(map[string]any{"alg": "none", "typ": "JWT"}) + "." + enc(claims) + ".x"
}

// Resolver answers DNS TXT lookups from a map.
type Resolver map[string][]string

// LookupTXT implements audit.Resolver.
func (r Resolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	if v, ok := r[name]; ok {
		return v, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}
