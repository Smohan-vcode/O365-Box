package extensions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
)

// The extensions-in-use report (T14-20): which Edge extensions company
// devices run, on how many devices, and whether the Edge policy in the
// tenant allows them. It only reads.

// Edge policy names the box creates (expected-state/setting-ids).
const (
	WindowsPolicy = "Box - Windows - Microsoft Edge"
	MacPolicy     = "Box macOS Edge"
)

// InventoryQuery is the Advanced hunting query over Defender Vulnerability
// Management's browser extension inventory.
const InventoryQuery = `DeviceTvmBrowserExtensions
| where BrowserName =~ "edge"
| summarize Devices = dcount(DeviceId), Name = take_any(ExtensionName), Vendor = take_any(ExtensionVendor) by ExtensionId
| order by Devices desc`

// InventoryNeeds is what reading installed extensions takes. Microsoft
// Graph has no other source: Intune's discovered apps do not list browser
// extensions, and the Edge management service has no Graph API.
const InventoryNeeds = "the application permission ThreatHunting.Read.All (not in expected-state/app-permissions.yaml: proposed, not granted) " +
	"and Microsoft Defender Vulnerability Management's browser extensions assessment (the Defender Vulnerability Management add-on; " +
	"not part of Microsoft 365 Business Premium). Intune's discovered apps do not list Edge extensions"

// Live is one platform's Edge extension policy as the tenant holds it.
type Live struct {
	Platform string   `json:"platform"`
	Policy   string   `json:"policy"`
	Found    bool     `json:"found"`
	Block    []string `json:"block,omitempty"`
	Allow    []string `json:"allow,omitempty"`
	Force    []string `json:"force,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// Status says what the live policy does with an extension id.
func (l Live) Status(id string) string {
	switch {
	case l.Error != "":
		return "not read"
	case !l.Found:
		return "no Edge policy"
	case slices.Contains(l.Force, id):
		return "installed for everyone"
	case slices.Contains(l.Block, id):
		return "blocked"
	case slices.Contains(l.Allow, id):
		return "allowed"
	case slices.Contains(l.Block, "*"):
		return "blocked"
	}
	return "allowed (not blocked)"
}

// Summary is the live policy in a few words.
func (l Live) Summary() string {
	switch {
	case l.Error != "":
		return l.Platform + ": policy not read (" + l.Error + ")"
	case !l.Found:
		return l.Platform + ": no \"" + l.Policy + "\" policy in Intune"
	case slices.Contains(l.Block, "*"):
		return fmt.Sprintf("%s: every extension blocked except %d allowed; %d installed for everyone", l.Platform, len(l.Allow), len(l.Force))
	}
	return fmt.Sprintf("%s: extensions not blocked; %d installed for everyone", l.Platform, len(l.Force))
}

// LiveFromSettings reads the extension lists out of a settings-catalogue
// policy's settings (GET .../configurationPolicies/{id}/settings).
func LiveFromSettings(platform, policy string, settings []map[string]any) Live {
	l := Live{Platform: platform, Policy: policy, Found: true}
	var walk func(v any, kind string)
	walk = func(v any, kind string) {
		switch x := v.(type) {
		case map[string]any:
			k := kind
			if id, _ := x["settingDefinitionId"].(string); id != "" {
				for _, c := range []string{"blocklist", "allowlist", "forcelist"} {
					if strings.Contains(id, "extensioninstall"+c) {
						k = c
					}
				}
			}
			if vals, ok := x["simpleSettingCollectionValue"].([]any); ok && k != "" {
				for _, sv := range vals {
					m, _ := sv.(map[string]any)
					s, _ := m["value"].(string)
					if s = strings.TrimSpace(s); s == "" {
						continue
					}
					if k == "forcelist" {
						s, _, _ = strings.Cut(s, ";") // id;update URL
					}
					switch k {
					case "blocklist":
						l.Block = append(l.Block, s)
					case "allowlist":
						l.Allow = append(l.Allow, s)
					case "forcelist":
						l.Force = append(l.Force, s)
					}
				}
			}
			for _, c := range x {
				walk(c, k)
			}
		case []any:
			for _, c := range x {
				walk(c, kind)
			}
		}
	}
	for _, s := range settings {
		walk(s, "")
	}
	return l
}

// ReadLive reads one Edge policy by name.
func ReadLive(ctx context.Context, gc *graph.Client, beta, platform, name string) Live {
	l := Live{Platform: platform, Policy: name}
	var list struct {
		Value []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"value"`
	}
	q := beta + "/deviceManagement/configurationPolicies?$select=id,name&$filter=" + url.QueryEscape("name eq '"+strings.ReplaceAll(name, "'", "''")+"'")
	if err := gc.Get(ctx, q, &list); err != nil {
		l.Error = err.Error()
		return l
	}
	if len(list.Value) == 0 {
		return l
	}
	settings, err := graph.List[map[string]any](ctx, gc, beta+"/deviceManagement/configurationPolicies/"+list.Value[0].ID+"/settings")
	if err != nil {
		l.Error = err.Error()
		return l
	}
	return LiveFromSettings(platform, name, settings)
}

// InUse is one extension found on devices.
type InUse struct {
	ID      string `json:"extension_id"`
	Name    string `json:"name"`
	Vendor  string `json:"vendor,omitempty"`
	Devices int    `json:"devices"`
}

// Inventory is the read of installed extensions.
type Inventory struct {
	Read   bool    `json:"read"`
	Reason string  `json:"reason,omitempty"` // why it was not read, and what it needs
	Query  string  `json:"query"`
	Items  []InUse `json:"items,omitempty"`
}

// ReadInventory runs the Advanced hunting query. A refusal (permission,
// licence or table missing) is a not-read inventory with the reason.
func ReadInventory(ctx context.Context, gc *graph.Client, v1 string) Inventory {
	inv := Inventory{Query: "POST " + v1 + "/security/runHuntingQuery: " + InventoryQuery}
	var res struct {
		Results []map[string]any `json:"results"`
	}
	if err := gc.Do(ctx, http.MethodPost, v1+"/security/runHuntingQuery", map[string]string{"Query": InventoryQuery}, &res); err != nil {
		inv.Reason = "Microsoft Graph refused the read (" + shortErr(err) + "); it needs " + InventoryNeeds
		return inv
	}
	inv.Read = true
	for _, r := range res.Results {
		id, _ := r["ExtensionId"].(string)
		if id == "" {
			continue
		}
		n := 0
		switch d := r["Devices"].(type) {
		case float64:
			n = int(d)
		case json.Number:
			x, _ := d.Int64()
			n = int(x)
		}
		name, _ := r["Name"].(string)
		vendor, _ := r["Vendor"].(string)
		inv.Items = append(inv.Items, InUse{ID: strings.ToLower(id), Name: name, Vendor: vendor, Devices: n})
	}
	return inv
}

func shortErr(err error) string {
	if ge, ok := err.(*graph.Error); ok {
		m := ge.Message
		if i := strings.Index(m, ". "); i > 0 {
			m = m[:i]
		}
		return fmt.Sprintf("HTTP %d %s: %s", ge.Status, ge.Code, m)
	}
	return err.Error()
}

// Row is one line of the report.
type Row struct {
	ID            string `json:"extension_id"`
	Name          string `json:"name"`
	Devices       string `json:"devices"` // a number, or "unknown" when the inventory was not read
	Windows       string `json:"windows"`
	MacOS         string `json:"macos"`
	Configuration string `json:"configuration"` // approved | approved, for everyone | not approved
}

// Rows joins the inventory, the approved lists and the live policies: one
// row per extension found on devices (most devices first), then each
// approved extension not found.
func Rows(inv Inventory, l Lists, names map[string]string, win, mac Live) []Row {
	byID := map[string]*Row{}
	var order []string
	add := func(id, name string) *Row {
		if r, ok := byID[id]; ok {
			if r.Name == "" {
				r.Name = name
			}
			return r
		}
		r := &Row{ID: id, Name: name, Devices: "unknown"}
		byID[id] = r
		order = append(order, id)
		return r
	}
	for _, it := range inv.Items {
		r := add(it.ID, it.Name)
		r.Devices = fmt.Sprint(it.Devices)
	}
	for _, id := range append(slices.Clone(l.Allow), l.Forced...) {
		r := add(id, names[id])
		if inv.Read && r.Devices == "unknown" {
			r.Devices = "0"
		}
	}
	for _, id := range order {
		r := byID[id]
		r.Windows, r.MacOS = win.Status(id), mac.Status(id)
		switch {
		case slices.Contains(l.Forced, id):
			r.Configuration = "approved, for everyone"
		case slices.Contains(l.Allow, id):
			r.Configuration = "approved"
		default:
			r.Configuration = "not approved"
		}
	}
	out := make([]Row, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
}

// Proposal is a permission the report would need, proposed for the
// owner's decision and never granted by the box (CLAUDE.md, SPEC §24.3).
type Proposal struct {
	Permission string `json:"permission"`
	Bundle     string `json:"bundle"`
	Why        string `json:"why"`
	Needs      string `json:"needs"`
}

// InventoryProposal is what reading the installed extensions would take
// (upgrades/browser-extension-inventory.md).
func InventoryProposal() []Proposal {
	return []Proposal{{
		Permission: "Microsoft Graph application permission ThreatHunting.Read.All",
		Bundle:     "a new bundle device-inventory, needed only when the owner chooses the extension inventory",
		Why:        "runs the read-only Advanced hunting query over DeviceTvmBrowserExtensions (POST /security/runHuntingQuery) for the extensions-in-use report",
		Needs:      "the Microsoft Defender Vulnerability Management add-on (browser extensions assessment), which Microsoft 365 Business Premium does not include; see upgrades/browser-extension-inventory.md",
	}}
}
