// Package extensions handles Microsoft Edge extensions on company devices
// (T14-18, T14-19, T14-20): staff ask for an extension in the
// ExtensionRequests list, the owner decides (SPEC §25: the box suggests,
// the owner decides), and an approval adds the extension id to the
// configuration's devices.browser.extension_allowlist, which the Edge
// policies read. Nothing here adds an extension without that decision.
package extensions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Request states (engine/register/lists.yaml enum request_state).
const (
	Requested = "requested"
	Approved  = "approved"
	Rejected  = "rejected"
)

// List is the register list staff add their requests to.
const List = "ExtensionRequests"

var idForm = regexp.MustCompile(`^[a-p]{32}$`)

// ValidID reports whether s is an extension id: 32 letters a to p, as
// Microsoft Edge (and Chrome) form them.
func ValidID(s string) bool { return idForm.MatchString(s) }

// CheckID returns an error that says what an extension id looks like.
func CheckID(s string) error {
	if ValidID(s) {
		return nil
	}
	return fmt.Errorf("%q is not an extension id: an id is 32 lowercase letters from a to p (edge://extensions shows it with developer mode on)", s)
}

// Request is one ExtensionRequests item.
type Request struct {
	Item               int    `json:"item"` // the SharePoint item id: the request's number
	ExtensionID        string `json:"extension_id"`
	Name               string `json:"extension_name"` // as the requester describes it
	StoreName          string `json:"store_name,omitempty"`
	Reason             string `json:"reason"`
	Requester          string `json:"requester"`
	InstallForEveryone bool   `json:"install_for_everyone,omitempty"` // the requester asks; the owner decides
	State              string `json:"state"`
	DecidedBy          string `json:"decided_by,omitempty"`
	DecidedAt          string `json:"decided_at,omitempty"`
	DecisionReason     string `json:"decision_reason,omitempty"`
	Created            string `json:"created,omitempty"`
}

// Label is the extension as the owner sees it: the store's name when it
// was looked up, else the requester's description with the id.
func (r Request) Label() string {
	switch {
	case r.StoreName != "":
		return r.StoreName
	case r.Name != "":
		return r.Name + " (as described by the requester; not looked up in the store)"
	}
	return "extension " + r.ExtensionID + " (no name given; not looked up in the store)"
}

// Pending lists the requests waiting for the owner, oldest first.
func Pending(rs []Request) []Request {
	var out []Request
	for _, r := range rs {
		if r.State == "" || r.State == Requested {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b Request) int { return a.Item - b.Item })
	return out
}

// StoreURL is the Microsoft Edge Add-ons product lookup by extension id.
// Only the id is sent.
var StoreURL = "https://microsoftedge.microsoft.com/addons/getproductdetailsbycrxid/"

// LookupName asks the Edge Add-ons store for an extension's name. It
// returns "" and an error when the store cannot be reached or does not
// know the id; callers then show the id and the requester's description.
func LookupName(ctx context.Context, hc *http.Client, id string) (string, error) {
	if err := CheckID(id); err != nil {
		return "", err
	}
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, StoreURL+id, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("the Edge Add-ons store is not reachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the Edge Add-ons store answered HTTP %d for %s", resp.StatusCode, id)
	}
	var p struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&p); err != nil {
		return "", fmt.Errorf("the Edge Add-ons store answer: %w", err)
	}
	if strings.TrimSpace(p.Name) == "" {
		return "", fmt.Errorf("the Edge Add-ons store has no extension %s", id)
	}
	return strings.TrimSpace(p.Name), nil
}

// Lists are the configuration's extension decisions
// (devices.browser.extension_allowlist and forced_extensions).
type Lists struct {
	Allow  []string
	Forced []string
}

// Approve adds id to the allow list and, when everyone is set, to the
// forced list. It reports whether anything changed.
func (l Lists) Approve(id string, everyone bool) (Lists, bool) {
	out := Lists{Allow: slices.Clone(l.Allow), Forced: slices.Clone(l.Forced)}
	changed := false
	if !slices.Contains(out.Allow, id) {
		out.Allow = append(out.Allow, id)
		changed = true
	}
	if everyone && !slices.Contains(out.Forced, id) {
		out.Forced = append(out.Forced, id)
		changed = true
	}
	return out, changed
}

// devicesBlock is the configuration's devices section, written by the box.
func devicesBlock(l Lists) string {
	var b strings.Builder
	b.WriteString("devices:\n  browser:\n")
	b.WriteString("    # Microsoft Edge extensions the owner approved (box extensions approve).\n")
	b.WriteString("    # Each change is a new configuration version: plan, approve, apply.\n")
	list := func(key string, ids []string) {
		if len(ids) == 0 {
			b.WriteString("    " + key + ": []\n")
			return
		}
		b.WriteString("    " + key + ":\n")
		for _, id := range ids {
			b.WriteString("    - " + id + "\n")
		}
	}
	list("extension_allowlist", l.Allow)
	list("forced_extensions", l.Forced)
	return b.String()
}

// ReadLists returns the extension lists of a configuration document.
func ReadLists(doc []byte) (Lists, error) {
	var c struct {
		Devices struct {
			Browser struct {
				Allow  []string `yaml:"extension_allowlist"`
				Forced []string `yaml:"forced_extensions"`
			} `yaml:"browser"`
		} `yaml:"devices"`
	}
	if err := yaml.Unmarshal(doc, &c); err != nil {
		return Lists{}, err
	}
	return Lists{Allow: c.Devices.Browser.Allow, Forced: c.Devices.Browser.Forced}, nil
}

// SetLists returns the configuration document with its devices section
// holding l. Every other line is kept as it is: the devices section is
// replaced in place, or added at the end.
func SetLists(doc []byte, l Lists) ([]byte, error) {
	for _, id := range append(slices.Clone(l.Allow), l.Forced...) {
		if err := CheckID(id); err != nil {
			return nil, err
		}
	}
	var n yaml.Node
	if err := yaml.Unmarshal(doc, &n); err != nil {
		return nil, err
	}
	if len(n.Content) == 0 || n.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("the configuration document is not a mapping")
	}
	root := n.Content[0]
	block := devicesBlock(l)
	lines := strings.SplitAfter(string(doc), "\n")
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i], root.Content[i+1]
		if k.Value != "devices" {
			continue
		}
		if k.Column != 1 {
			return nil, errors.New("the devices section is not at the start of a line")
		}
		if err := onlyBrowser(v); err != nil {
			return nil, err
		}
		first, last := k.Line, lastLine(v)
		if last < first {
			last = first
		}
		var out bytes.Buffer
		for _, s := range lines[:first-1] {
			out.WriteString(s)
		}
		out.WriteString(block)
		for _, s := range lines[last:] {
			out.WriteString(s)
		}
		return out.Bytes(), nil
	}
	out := string(doc)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return []byte(out + block), nil
}

// onlyBrowser refuses a devices section with keys the box would drop.
func onlyBrowser(v *yaml.Node) error {
	if v.Kind != yaml.MappingNode {
		if v.Kind == yaml.ScalarNode && (v.Tag == "!!null" || v.Value == "") {
			return nil
		}
		return errors.New("devices is not a mapping")
	}
	for i := 0; i+1 < len(v.Content); i += 2 {
		if v.Content[i].Value != "browser" {
			return fmt.Errorf("devices.%s is not a key the box writes; move it before approving", v.Content[i].Value)
		}
		b := v.Content[i+1]
		for j := 0; b.Kind == yaml.MappingNode && j+1 < len(b.Content); j += 2 {
			if k := b.Content[j].Value; k != "extension_allowlist" && k != "forced_extensions" {
				return fmt.Errorf("devices.browser.%s is not a key the box writes; move it before approving", k)
			}
		}
	}
	return nil
}

func lastLine(n *yaml.Node) int {
	l := n.Line
	for _, c := range n.Content {
		l = max(l, lastLine(c))
	}
	return l
}

// WriteFile replaces path with b atomically, keeping its mode.
func WriteFile(path string, b []byte) error {
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
