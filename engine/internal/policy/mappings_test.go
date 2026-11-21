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

func TestSettingsByStepVariantAuditForm(t *testing.T) {
	// A variant of a staged setting has its own audit form (T14-04): the
	// audit stage writes it, the enforce stage the variant's payload.
	m := Mapping{Status: "resolved", Step: "policy.entra", Payload: "p", PayloadAudit: "pa",
		Variants: map[string]Variant{"v": {Payload: "vp", PayloadAudit: "vpa"}, "n": {Notes: "notes only"}}}
	for _, tc := range []struct{ mode, variant, want string }{
		{ModeAudit, "", "pa"}, {ModeEnforce, "", "p"}, {ModeAudit, "v", "vpa"}, {ModeEnforce, "v", "vp"}, {ModeAudit, "n", "pa"},
	} {
		res := &Result{Applied: []AppliedCentre{{ID: "entra", Settings: []AppliedSetting{{ID: "ENT-X", Mode: tc.mode, Variant: tc.variant}}}}}
		got := SettingsByStep(res, map[string]Mapping{"ENT-X": m})["policy.entra"]
		if len(got) != 1 || got[0].Payload != tc.want {
			t.Errorf("mode %s variant %q: %+v, want payload %s", tc.mode, tc.variant, got, tc.want)
		}
	}
}
