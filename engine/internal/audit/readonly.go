package audit

import (
	"fmt"
	"net/http"
)

// GetOnly is an http.RoundTripper that sends GET requests and refuses every
// other method, so the audit's own Graph reads cannot change the tenant
// whatever the token allows (SPEC §26).
type GetOnly struct{ Next http.RoundTripper }

// RoundTrip implements http.RoundTripper.
func (g GetOnly) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return nil, fmt.Errorf("audit is read-only: %s %s refused", r.Method, r.URL.Path)
	}
	next := g.Next
	if next == nil {
		next = http.DefaultTransport
	}
	// Some Graph APIs (device local credentials) require a client name.
	r = r.Clone(r.Context())
	r.Header.Set("ocp-client-name", "box-audit")
	r.Header.Set("ocp-client-version", "1")
	return next.RoundTrip(r)
}

// ReadOnlyClient wraps hc (nil: the default client) with GetOnly.
func ReadOnlyClient(hc *http.Client) *http.Client {
	var next http.RoundTripper
	if hc != nil {
		next = hc.Transport
	}
	c := &http.Client{Transport: GetOnly{Next: next}}
	if hc != nil {
		c.Timeout = hc.Timeout
		c.Jar = hc.Jar
	}
	return c
}
