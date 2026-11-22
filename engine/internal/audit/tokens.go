package audit

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/auth"
)

// Resource is one workload the audit reads with its own delegated token
// (T16-06), listed in expected-state/audit-scopes.yaml.
type Resource struct {
	Name       string `yaml:"name"`     // exchange, compliance, sharepoint, teams
	Title      string `yaml:"title"`    // for messages
	Resource   string `yaml:"resource"` // https resource, application id, or {sharepoint_admin}
	Client     string `yaml:"client"`   // public client the token is issued to; empty: the Graph sign-in's
	Provider   string `yaml:"provider"` // the provider worker that gets the token
	Credential string `yaml:"credential"`
}

var (
	guid         = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	httpsHost    = regexp.MustCompile(`^https://[a-z0-9.-]+$`)
	resourceVars = map[string]bool{"{sharepoint_admin}": true}
	// Names a provider worker reads a resource token under. "graph" is the
	// audit's own read-only Graph token and is never replaced.
	credentialNames = map[string]bool{"app": true, "ipps": true, "teams": true, "teamsgraph": true}
)

// CheckResources validates the resources list.
func CheckResources(list []Resource) error {
	seen := map[string]bool{}
	slot := map[string]bool{}
	for _, r := range list {
		switch {
		case r.Name == "" || r.Provider == "":
			return fmt.Errorf("resource %q: name and provider are required", r.Name)
		case seen[r.Name]:
			return fmt.Errorf("resource %s is listed twice", r.Name)
		case !guid.MatchString(r.Resource) && !httpsHost.MatchString(r.Resource) && !resourceVars[r.Resource]:
			return fmt.Errorf("resource %s: %q is not an https host, an application id or {sharepoint_admin}", r.Name, r.Resource)
		case r.Client != "" && !guid.MatchString(r.Client):
			return fmt.Errorf("resource %s: client %q is not an application id", r.Name, r.Client)
		case !credentialNames[r.Credential]:
			return fmt.Errorf("resource %s: credential %q is not app, ipps, teams or teamsgraph", r.Name, r.Credential)
		case slot[r.Provider+"/"+r.Credential]:
			return fmt.Errorf("resource %s: provider %s already gets a %s token", r.Name, r.Provider, r.Credential)
		}
		seen[r.Name], slot[r.Provider+"/"+r.Credential] = true, true
	}
	return nil
}

// Scope is the resource's ".default" scope with the tenant's SharePoint
// admin host filled in, or an error when it cannot be.
func (r Resource) Scope(sharepointAdmin string) (string, error) {
	res := r.Resource
	if res == "{sharepoint_admin}" {
		if sharepointAdmin == "" {
			return "", fmt.Errorf("the tenant's SharePoint admin host is not known (its initial domain could not be read)")
		}
		res = sharepointAdmin
	}
	return res + "/.default", nil
}

// TokenSource is what Tokens reaches Entra through; tests replace it.
type TokenSource struct {
	// Redeem exchanges a refresh token for a token to scopes, for client.
	Redeem func(ctx context.Context, client, refresh string, scopes []string) (*auth.Token, error)
	// SignIn runs a device-code sign-in with client for scopes; title
	// names the workloads it is for, so the person knows which code is which.
	SignIn func(ctx context.Context, client string, scopes []string, title string) (*auth.Token, error)
}

// Tokens holds the audit's delegated tokens for Exchange Online, Security &
// Compliance, SharePoint and Teams, in memory only (R12). It asks for them
// only when Session.ResourcesAllowed, checks that each was issued to the same
// reader, renews them from the refresh token before they expire, and drops
// everything with Drop.
type Tokens struct {
	Session    Session
	MainClient string // the Graph sign-in's client
	Source     TokenSource
	Now        func() time.Time

	mu      sync.Mutex
	renew   sync.Mutex        // one renewal at a time
	refresh map[string]string // per client
	byName  map[string]*held
}

type held struct {
	res    Resource
	scope  string
	client string
	tok    *auth.Token
	reason string // why there is no token
}

// NewTokens starts from the Graph sign-in: its refresh token serves every
// resource whose client is empty.
func NewTokens(sess Session, mainClient string, main *auth.Token, src TokenSource) *Tokens {
	t := &Tokens{Session: sess, MainClient: mainClient, Source: src, refresh: map[string]string{}, byName: map[string]*held{}}
	if main != nil && main.Refresh != "" {
		t.refresh[mainClient] = main.Refresh
	}
	return t
}

func (t *Tokens) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// Acquire gets a token for each resource. A resource it cannot get keeps a
// reason (Reason) and its provider's settings are reported not read. Clients
// other than the Graph sign-in's take one more device-code sign-in each,
// shared by every resource of that client. progress, when set, is told of
// each outcome.
func (t *Tokens) Acquire(ctx context.Context, list []Resource, sharepointAdmin string, progress func(r Resource, err error)) {
	allowed, why := t.Session.ResourcesAllowed()
	for _, r := range list {
		h := &held{res: r, client: r.Client}
		if h.client == "" {
			h.client = t.MainClient
		}
		t.mu.Lock()
		t.byName[r.Name] = h
		t.mu.Unlock()
		var err error
		switch {
		case !allowed:
			err = fmt.Errorf("%s", why)
		default:
			h.scope, err = r.Scope(sharepointAdmin)
			if err == nil {
				err = t.obtain(ctx, h, groupTitle(list, h.client, r.Client == ""))
			}
		}
		if err != nil {
			h.reason = fmt.Sprintf("no read-only %s sign-in: %s", titleOf(r), err.Error())
		}
		if progress != nil {
			progress(r, err)
		}
	}
}

// obtain gets h's token: from the client's refresh token when one is held,
// else with a device-code sign-in for that client. Each token is checked
// against the first sign-in.
func (t *Tokens) obtain(ctx context.Context, h *held, title string) error {
	t.mu.Lock()
	rt := t.refresh[h.client]
	t.mu.Unlock()
	var tok *auth.Token
	var err error
	switch {
	case rt != "":
		if t.Source.Redeem == nil {
			return fmt.Errorf("no token endpoint")
		}
		tok, err = t.Source.Redeem(ctx, h.client, rt, []string{h.scope, "offline_access"})
	case h.client == t.MainClient:
		return fmt.Errorf("the first sign-in returned no refresh token")
	default:
		if t.Source.SignIn == nil {
			return fmt.Errorf("no device-code sign-in available")
		}
		tok, err = t.Source.SignIn(ctx, h.client, []string{h.scope, "offline_access"}, title)
	}
	if err != nil {
		return err
	}
	if err := t.Session.SameReader(tok.Claims); err != nil {
		tok.Drop()
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if tok.Refresh != "" {
		t.refresh[h.client] = tok.Refresh
	}
	h.tok = tok
	return nil
}

// Has reports whether the resource has a token.
func (t *Tokens) Has(name string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	h := t.byName[name]
	return h != nil && h.tok != nil
}

// Reason says why a resource has no token ("" when it has one or was not
// asked for).
func (t *Tokens) Reason(name string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if h := t.byName[name]; h != nil && h.tok == nil {
		return h.reason
	}
	return ""
}

// Token returns the resource's access token, renewed from the refresh token
// when it has less than five minutes left.
func (t *Tokens) Token(ctx context.Context, name string) (string, error) {
	t.renew.Lock()
	defer t.renew.Unlock()
	t.mu.Lock()
	h := t.byName[name]
	var tok *auth.Token
	reason := ""
	if h != nil {
		tok, reason = h.tok, h.reason
	}
	t.mu.Unlock()
	if tok == nil {
		if reason != "" {
			return "", fmt.Errorf("%s", reason)
		}
		return "", fmt.Errorf("no %s token", name)
	}
	if exp := tok.ExpiresAt; exp.IsZero() || exp.Sub(t.now()) >= 5*time.Minute {
		return tok.Access, nil
	}
	err := t.obtain(ctx, h, "")
	t.mu.Lock()
	defer t.mu.Unlock()
	if err != nil {
		h.tok = nil
		h.reason = "renewing the " + titleOf(h.res) + " token: " + err.Error()
		tok.Drop()
		return "", fmt.Errorf("%s", h.reason)
	}
	if h.tok != tok {
		tok.Drop()
	}
	return h.tok.Access, nil
}

// ForProvider lists the credentials a provider worker gets: name (app, ipps,
// teams) to resource name, for the resources that have a token.
func (t *Tokens) ForProvider(provider string) map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := map[string]string{}
	for name, h := range t.byName {
		if h.res.Provider == provider && h.tok != nil {
			out[h.res.Credential] = name
		}
	}
	return out
}

// ProviderReason says why a provider's worker cannot read: the reasons of
// its resources that have no token, or "" when every one has a token.
func (t *Tokens) ProviderReason(provider string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []string
	for _, h := range t.byName {
		if h.res.Provider == provider && h.tok == nil && h.reason != "" {
			out = append(out, h.reason)
		}
	}
	return strings.Join(sortedUnique(out), "; ")
}

// Secrets lists every access and refresh token held, for redaction.
func (t *Tokens) Secrets() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []string
	for _, h := range t.byName {
		if h.tok != nil {
			out = append(out, h.tok.Access, h.tok.Refresh)
		}
	}
	for _, rt := range t.refresh {
		out = append(out, rt)
	}
	return out
}

// Drop clears every token and refresh token.
func (t *Tokens) Drop() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, h := range t.byName {
		h.tok.Drop()
		h.tok = nil
	}
	for k := range t.refresh {
		delete(t.refresh, k)
	}
}

func titleOf(r Resource) string {
	if r.Title != "" {
		return r.Title
	}
	return r.Name
}

// groupTitle names every resource one sign-in with client serves.
func groupTitle(list []Resource, client string, main bool) string {
	var names []string
	for _, r := range list {
		if r.Client == client || (main && r.Client == "") {
			names = append(names, titleOf(r))
		}
	}
	return strings.Join(names, " and ")
}

func sortedUnique(in []string) []string {
	sort.Strings(in)
	out := in[:0]
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}
