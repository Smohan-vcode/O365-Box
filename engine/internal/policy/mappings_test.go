package policy

import (
	"strings"
	"testing"
)

func repoCatalogue(t *testing.T) *Catalogue {
	t.Helper()
	c, err := Load("../../../policy", "policy")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCheckMappings(t *testing.T) {
	c := repoCatalogue(t)
	_, settings, _ := c.IDs()
	maps := map[string]Mapping{}
	file := map[string]string{}
	var staged, addon, plain string
	for _, id := range settings {
		st := c.Setting(id)
		m := Mapping{Status: "resolved", Provider: "identity", Step: "policy." + st.Centre, Method: "graph",
			Apply: "PATCH /x", Read: "GET /x", Check: "a == b", PayloadAudit: map[string]any{"state": "audit"}}
		if st.Tier == "addon" {
			m = Mapping{Status: "not-applied"}
			addon = id
		} else if st.Mode == "staged" && staged == "" {
			staged = id
		} else if plain == "" {
			plain = id
		}
		maps[id] = m
		file[id] = st.Centre + ".json"
	}
	if issues := c.CheckMappings(maps, file); len(issues) != 0 {
		t.Fatalf("complete mappings reported issues: %v", issues)
	}
	bad := map[string]Mapping{}
	for k, v := range maps {
		bad[k] = v
	}
	delete(bad, plain)
	s := bad[staged]
	s.PayloadAudit = nil
	bad[staged] = s
	bad["XXX-999"] = Mapping{Status: "resolved"}
	file["XXX-999"] = "x.json"
	g := bad[addon]
	g.Status = "gate"
	bad[addon] = g
	want := map[string]string{plain: "has no mapping", staged: "needs payload_audit", "XXX-999": "not in the catalogue", addon: "needs notes"}
	issues := c.CheckMappings(bad, file)
	for id, msg := range want {
		found := false
		for _, i := range issues {
			found = found || (i.Path == id && strings.Contains(i.Message, msg))
		}
		if !found {
			t.Errorf("want %s: %s in %v", id, msg, issues)
		}
	}
	cov, err := c.CoverageMap(maps)
	if err != nil || !strings.Contains(string(cov), "apply_by: identity step policy.") {
		t.Errorf("coverage map: %v\n%s", err, cov[:200])
	}
}
