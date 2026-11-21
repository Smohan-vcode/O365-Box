package audit

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Smohan-vcode/O365-Box/engine/internal/audit/audittest"
	"github.com/Smohan-vcode/O365-Box/engine/internal/policy"
)

func catalogue(t *testing.T) (*policy.Catalogue, map[string]policy.Mapping) {
	t.Helper()
	cat, err := policy.Load(filepath.Join(repoRoot, "policy"), "policy")
	if err != nil {
		t.Fatal(err)
	}
	maps, _, err := policy.LoadMappings(filepath.Join(repoRoot, "expected-state", "setting-ids"))
	if err != nil {
		t.Fatal(err)
	}
	return cat, maps
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// fakeOutcomes stands for the policy steps' verify in audit mode: in
// policy.entra the first setting differs and the second could not be
// resolved; policy.intune-windows verifies; policy.m365-admin failed.
func fakeOutcomes(t *testing.T, bySteps map[string][]policy.StepSetting) (map[string]StepOutcome, string, string) {
	entra := bySteps["policy.entra"]
	if len(entra) < 3 || len(bySteps["policy.intune-windows"]) == 0 || len(bySteps["policy.m365-admin"]) == 0 {
		t.Fatalf("catalogue steps changed: %d entra settings", len(entra))
	}
	failID, skipID := entra[0].ID, entra[1].ID
	return map[string]StepOutcome{
		"policy.entra": {Step: "policy.entra", Provider: "identity", Ran: true, Verify: raw(map[string]any{"ok": false,
			"drift":   []any{map[string]any{"action": "create", "target": failID + " policy X", "detail": "absent"}},
			"skipped": []any{map[string]any{"id": skipID, "reason": "group grp-staff does not exist"}}})},
		"policy.intune-windows": {Step: "policy.intune-windows", Provider: "intune", Ran: true, Verify: raw(map[string]any{"ok": true})},
		"policy.m365-admin":     {Step: "policy.m365-admin", Provider: "identity", Reason: "verify: HTTP 403 Authorization_RequestDenied"},
	}, failID, skipID
}

func TestReadSettingsFromVerify(t *testing.T) {
	cat, maps := catalogue(t)
	by := AuditSelection(cat, maps)
	out, failID, skipID := fakeOutcomes(t, by)
	s := ReadSettings(cat, maps, by, out)
	if n, want := len(s), cat.SettingCount(); n != want {
		t.Fatalf("%d settings read, catalogue has %d", n, want)
	}
	if s[failID].Status != Fail || s[skipID].Status != NotRead || !strings.Contains(s[skipID].Detail, "grp-staff") {
		t.Errorf("entra: %+v %+v", s[failID], s[skipID])
	}
	if pass := s[by["policy.entra"][2].ID]; pass.Status != Pass {
		t.Errorf("unmentioned entra setting = %+v", pass)
	}
	for _, ss := range by["policy.intune-windows"] {
		if s[ss.ID].Status != Pass {
			t.Errorf("%s = %+v", ss.ID, s[ss.ID])
		}
	}
	for _, ss := range by["policy.m365-admin"] {
		if s[ss.ID].Status != NotRead || !strings.Contains(s[ss.ID].Detail, "403") {
			t.Errorf("%s = %+v", ss.ID, s[ss.ID])
		}
	}
	for id, sr := range s {
		m := maps[id]
		switch {
		case m.Provider == "exchange" && m.Status == "resolved" && !strings.HasPrefix(m.Step, "policy.purview"):
			if sr.Status != NotRead || !strings.Contains(sr.Detail, "Exchange Online PowerShell") {
				t.Errorf("%s (exchange) = %+v", id, sr)
			}
		case m.Status == "gate":
			if sr.Status != NotRead || !strings.Contains(sr.Detail, "a person confirms") {
				t.Errorf("%s (gate) = %+v", id, sr)
			}
		case m.Status == "not-applied":
			if sr.Status != NotRead || !strings.Contains(sr.Detail, "add-on") {
				t.Errorf("%s (add-on) = %+v", id, sr)
			}
		}
		if sr.Status != NotRead && !GraphProviders[sr.Provider] {
			t.Errorf("%s read through %s, which the audit does not run", id, sr.Provider)
		}
	}
	// A setting the step left out with a warning is not read, never pass.
	win := by["policy.intune-windows"][0].ID
	warned := map[string]StepOutcome{"policy.intune-windows": {Ran: true, Step: "policy.intune-windows", Verify: raw(map[string]any{"ok": true}),
		Warnings: []string{"skipped " + win + " (windows): needs value(s) not in the configuration"}}}
	if sr := ReadSettings(cat, maps, by, warned)[win]; sr.Status != NotRead || !strings.Contains(sr.Detail, "needs value") {
		t.Errorf("warned setting = %+v", sr)
	}
	// A drift item naming no setting leaves the step's other settings not read.
	loose := map[string]StepOutcome{"policy.intune-windows": {Ran: true, Step: "policy.intune-windows",
		Verify: raw(map[string]any{"ok": false, "drift": []any{map[string]any{"action": "update", "target": "assignment of Box - Windows", "detail": "a -> b"}}})}}
	s = ReadSettings(cat, maps, by, loose)
	if sr := s[by["policy.intune-windows"][0].ID]; sr.Status != NotRead || !strings.Contains(sr.Detail, "not tied to one setting") {
		t.Errorf("loose drift = %+v", sr)
	}
}

// TestAuditPackEndToEnd reads the recorded tenant, assesses SOC 2 and
// writes the pack (T16-03, T16-04 exit criteria, offline).
func TestAuditPackEndToEnd(t *testing.T) {
	cat, maps := catalogue(t)
	r, _ := testReader(t)
	invs := r.ReadAll(context.Background())
	by := AuditSelection(cat, maps)
	outcomes, failID, _ := fakeOutcomes(t, by)
	settings := ReadSettings(cat, maps, by, outcomes)
	fw := cat.Framework("soc2")
	a := Assess(cat, fw, settings, invs)

	valid := map[string]bool{Met: true, PartlyMet: true, NotMet: true, DocumentNeeded: true, NotRead: true}
	if len(a.Controls) != len(fw.Controls) || len(a.Controls) == 0 {
		t.Fatalf("%d controls assessed, framework has %d", len(a.Controls), len(fw.Controls))
	}
	for _, c := range a.Controls {
		if !valid[c.Status] {
			t.Errorf("%s status %q", c.ID, c.Status)
		}
		if c.Status != Met && c.Gap == "" {
			t.Errorf("%s is %s without a gap", c.ID, c.Status)
		}
	}
	if st := a.Requirements["IAM-01"]; st == nil || st.Status != PartlyMet || !strings.Contains(st.Gap, failID) && !strings.Contains(st.Gap, "not registered") {
		t.Errorf("IAM-01 = %+v", st)
	}

	parent := t.TempDir()
	dir, err := CheckOutDir(repoRoot, filepath.Join(parent, "pack"))
	if err != nil {
		t.Fatal(err)
	}
	p := &Pack{Tenant: audittest.Tenant, Organisation: "Fernhill Labs", Subject: "auditor@fernhill.example", ReadBy: "box audit test",
		Period: r.Period, ReadAt: testNow, Framework: fw, Assessment: a, Inventories: invs, Settings: settings, Steps: outcomes,
		Notes: []string{"recorded responses"}}
	files, err := Write(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing outside the chosen folder.
	if entries, _ := os.ReadDir(parent); len(entries) != 1 || entries[0].Name() != "pack" {
		t.Errorf("written next to the pack: %v", entries)
	}
	for _, need := range []string{"index.html", "controls.csv", "gaps.md", ManifestFile, "evidence/settings/all-settings.json", "evidence/settings/policy.entra.json"} {
		if _, err := os.Stat(filepath.Join(dir, need)); err != nil {
			t.Errorf("%s missing", need)
		}
	}
	// Every control has its evidence files, each stamped with tenant, time and query.
	for _, c := range a.Controls {
		if len(c.Evidence) == 0 {
			t.Errorf("%s has no evidence files", c.ID)
		}
		for _, f := range c.Evidence {
			if !strings.HasPrefix(f, "evidence/"+c.ID+"/") {
				t.Errorf("%s evidence %s outside its folder", c.ID, f)
			}
		}
	}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.HasPrefix(f, "evidence/") && strings.HasSuffix(f, ".json"):
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			q, _ := m["queries"].([]any)
			if m["tenant_id"] != audittest.Tenant || m["read_at"] == nil || m["read_at"] == "" || len(q) == 0 {
				t.Errorf("%s is not stamped: tenant %v, read_at %v, queries %v", f, m["tenant_id"], m["read_at"], m["queries"])
			}
		case strings.HasSuffix(f, ".csv") && f != "controls.csv":
			if !strings.HasPrefix(string(b), "# tenant_id: "+audittest.Tenant+"\n# read_at: ") || !strings.Contains(string(b), "# query: ") {
				t.Errorf("%s is not stamped", f)
			}
		}
		if regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.`).Match(b) {
			t.Errorf("%s holds a token", f)
		}
	}
	// controls.csv: one row per control.
	cf, _ := os.Open(filepath.Join(dir, "controls.csv"))
	rows, err := csv.NewReader(cf).ReadAll()
	cf.Close()
	if err != nil || len(rows) != len(fw.Controls)+1 || rows[0][3] != "status" {
		t.Errorf("controls.csv: %d rows, %v", len(rows), err)
	}
	// index.html is self-contained.
	html, _ := os.ReadFile(filepath.Join(dir, "index.html"))
	for _, bad := range []string{"<script", "<link", " src=", `href="http`, "@import", "url("} {
		if strings.Contains(string(html), bad) {
			t.Errorf("index.html uses an external asset or script (%s)", bad)
		}
	}
	if !strings.Contains(string(html), "CC6.1") || !strings.Contains(string(html), "forwarding") {
		t.Error("index.html lacks controls or inventories")
	}
	// gaps.md is ordered by risk, highest first, each with a fix.
	gaps := a.Gaps()
	for i := 1; i < len(gaps); i++ {
		if gaps[i].Requirement.Risk > gaps[i-1].Requirement.Risk {
			t.Errorf("gaps out of order at %d", i)
		}
	}
	md, _ := os.ReadFile(filepath.Join(dir, "gaps.md"))
	if len(gaps) == 0 || strings.Count(string(md), "Fix with Company in a Box") < len(gaps)/2 {
		t.Errorf("gaps.md: %d gaps, %d fixes", len(gaps), strings.Count(string(md), "Fix with Company in a Box"))
	}
	// The manifest verifies, and catches a changed, a missing and an added file.
	if problems, err := VerifyManifest(dir); err != nil || len(problems) != 0 {
		t.Fatalf("manifest: %v %v", problems, err)
	}
	_ = os.WriteFile(filepath.Join(dir, "gaps.md"), append(md, '!'), 0o600)
	_ = os.Remove(filepath.Join(dir, "controls.csv"))
	_ = os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("x"), 0o600)
	problems, _ := VerifyManifest(dir)
	if strings.Join(problems, "|") != "controls.csv: missing|extra.txt: not in the manifest|gaps.md: changed" {
		t.Errorf("tampering found = %v", problems)
	}
}

func TestCheckOutDir(t *testing.T) {
	if _, err := CheckOutDir(repoRoot, filepath.Join(repoRoot, "tmp-pack")); err == nil {
		t.Error("a folder inside the repository was accepted")
	}
	if _, err := CheckOutDir(repoRoot, ""); err == nil {
		t.Error("an empty --out was accepted")
	}
	parent := t.TempDir()
	// A link from outside into the repository is still inside it.
	abs, _ := filepath.Abs(repoRoot)
	link := filepath.Join(parent, "link")
	if err := os.Symlink(abs, link); err == nil {
		if _, err := CheckOutDir(repoRoot, filepath.Join(link, "pack")); err == nil {
			t.Error("a path through a link into the repository was accepted")
		}
	}
	full := filepath.Join(parent, "full")
	_ = os.MkdirAll(full, 0o700)
	_ = os.WriteFile(filepath.Join(full, "a"), nil, 0o600)
	if _, err := CheckOutDir(repoRoot, full); err == nil {
		t.Error("a folder with files was accepted")
	}
	if _, err := CheckOutDir(repoRoot, filepath.Join(parent, "new")); err != nil {
		t.Error(err)
	}
}
