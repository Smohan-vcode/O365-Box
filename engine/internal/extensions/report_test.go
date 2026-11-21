package extensions

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Smohan-vcode/O365-Box/engine/internal/graph"
)

func strValue(v string) map[string]any {
	return map[string]any{"@odata.type": "#microsoft.graph.deviceManagementConfigurationStringSettingValue", "value": v}
}

func TestLiveFromSettings(t *testing.T) {
	const w = "device_vendor_msft_policy_config_microsoft_edge~policy~microsoft_edge~extensions_extensioninstall"
	choice := func(kind string, vals ...string) map[string]any {
		var c []any
		for _, v := range vals {
			c = append(c, strValue(v))
		}
		return map[string]any{"settingInstance": map[string]any{"settingDefinitionId": w + kind, "choiceSettingValue": map[string]any{
			"value": w + kind + "_1", "children": []any{map[string]any{"settingDefinitionId": w + kind + "_" + "extensioninstall" + kind + "desc", "simpleSettingCollectionValue": c}}}}}
	}
	win := LiveFromSettings("Windows", WindowsPolicy, []map[string]any{choice("blocklist", "*"), choice("allowlist", idA), choice("forcelist", idB+";https://edge.example/crx")})
	if strings.Join(win.Block, ",") != "*" || strings.Join(win.Allow, ",") != idA || strings.Join(win.Force, ",") != idB {
		t.Fatalf("windows %+v", win)
	}
	if win.Status(idA) != "allowed" || win.Status(idB) != "installed for everyone" || win.Status("cccccccccccccccccccccccccccccccc") != "blocked" {
		t.Errorf("statuses %s %s", win.Status(idA), win.Status(idB))
	}
	mac := LiveFromSettings("macOS", MacPolicy, []map[string]any{{"settingInstance": map[string]any{
		"settingDefinitionId": "com.apple.managedclient.preferences_extensioninstallallowlist", "simpleSettingCollectionValue": []any{strValue(idA)}}}})
	if mac.Status(idB) != "allowed (not blocked)" || !strings.Contains(mac.Summary(), "not blocked") {
		t.Errorf("mac %+v", mac)
	}
	if (Live{Platform: "x", Policy: "p"}).Status(idA) != "no Edge policy" || (Live{Error: "403"}).Status(idA) != "not read" {
		t.Error("missing or unread policy")
	}
}

func TestReadInventoryAndRows(t *testing.T) {
	refuse := true
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/security/runHuntingQuery") || !strings.Contains(body["Query"], "DeviceTvmBrowserExtensions") {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if refuse {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":{"code":"Forbidden","message":"Missing application roles. API required roles: ThreatHunting.Read.All"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"results":[{"ExtensionId":"`+strings.ToUpper(idB)+`","Name":"Other helper","Vendor":"Example","Devices":3}]}`)
	}))
	defer srv.Close()
	gc := &graph.Client{HTTP: srv.Client(), Token: func() string { return "t" }}
	ctx := context.Background()
	inv := ReadInventory(ctx, gc, srv.URL+"/v1.0")
	if inv.Read || !strings.Contains(inv.Reason, "ThreatHunting.Read.All") || !strings.Contains(inv.Reason, "proposed, not granted") {
		t.Fatalf("refused read %+v", inv)
	}
	live := Live{Platform: "macOS", Policy: MacPolicy, Found: true, Block: []string{"*"}, Allow: []string{idA}}
	rows := Rows(inv, Lists{Allow: []string{idA}}, map[string]string{idA: "Example Notes"}, Live{Platform: "Windows", Policy: WindowsPolicy}, live)
	if len(rows) != 1 || rows[0].Devices != "unknown" || rows[0].MacOS != "allowed" || rows[0].Windows != "no Edge policy" || rows[0].Configuration != "approved" {
		t.Fatalf("rows when not read %+v", rows)
	}
	refuse = false
	inv = ReadInventory(ctx, gc, srv.URL+"/v1.0")
	if !inv.Read || len(inv.Items) != 1 || inv.Items[0].ID != idB || inv.Items[0].Devices != 3 {
		t.Fatalf("inventory %+v", inv)
	}
	rows = Rows(inv, Lists{Allow: []string{idA}}, nil, live, live)
	if len(rows) != 2 || rows[0].ID != idB || rows[0].MacOS != "blocked" || rows[0].Configuration != "not approved" || rows[1].Devices != "0" {
		t.Fatalf("rows %+v", rows)
	}
	if p := InventoryProposal(); len(p) != 1 || !strings.Contains(p[0].Permission, "ThreatHunting.Read.All") {
		t.Errorf("proposal %+v", p)
	}
}
