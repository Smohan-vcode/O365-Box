package cli

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Smohan-vcode/O365-Box/engine/internal/events"
)

// TestEndToEnd drives the real CLI, orchestrator and pwsh workers against
// a demo provider that keeps "tenant" resources in a JSON file.
func TestEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh not installed")
	}
	dir := t.TempDir()
	store := filepath.Join(dir, "tenant.json")
	gateFile := filepath.Join(dir, "gate-open")
	t.Setenv("BOX_PROVIDER_DEMO_STORE", store)
	t.Setenv("BOX_PROVIDER_DEMO_GATE", gateFile)
	t.Setenv("BOX_PROVIDER_DEMO_FAIL", "")
	common := []string{"--config", "../../../examples/acme/box-config.yaml", "--state", filepath.Join(dir, "state.json"),
		"--providers", "testdata/e2e/providers", "--gates", "testdata/e2e/gates.yaml"}
	cmd := func(args ...string) []string { return append(args, common...) }

	stream := func(args ...string) (int, []events.Event) {
		t.Helper()
		code, out := box(t, append(cmd(args...), "--json")...)
		var evs []events.Event
		sc := bufio.NewScanner(strings.NewReader(out))
		for sc.Scan() {
			var e events.Event
			if json.Unmarshal(sc.Bytes(), &e) == nil && e.Ev != "" {
				evs = append(evs, e)
			}
		}
		return code, evs
	}
	of := func(evs []events.Event, kind string) map[string]events.Event {
		m := map[string]events.Event{}
		for _, e := range evs {
			if e.Ev == kind {
				m[e.Step] = e
			}
		}
		return m
	}
	finished := func(evs []events.Event) events.Event {
		for _, e := range evs {
			if e.Ev == events.RunFinished {
				return e
			}
		}
		t.Fatal("no run.finished event")
		return events.Event{}
	}

	// 1. plan changes nothing and shows every step's changes.
	code, evs := stream("plan")
	if code != ExitOK {
		t.Fatalf("plan exit %d", code)
	}
	planned := of(evs, events.StepPlanned)
	if _, ok := planned["demo.sales"]; ok {
		t.Error("sales step planned although module sales is off")
	}
	if _, ok := planned["demo.basic"]; ok {
		t.Error("fallback planned although business_premium has conditional_access")
	}
	if ch := planned["demo.groups"].Data["changes"].([]any); len(ch) != 7 {
		t.Errorf("demo.groups planned %d changes, want 7 groups", len(ch))
	}
	if _, err := os.Stat(store); err == nil {
		t.Fatal("plan wrote to the tenant")
	}

	// 1a. nothing is applied before the owner approves (SPEC §25.1).
	code, evs = stream("up", "--yes")
	if code != ExitGate {
		t.Fatalf("up before approval: exit %d, want %d", code, ExitGate)
	}
	if na, _ := finished(evs).Data["needs_approval"].([]any); len(na) == 0 {
		t.Error("run.finished names no step needing approval")
	}
	if w := of(evs, events.GateWaiting)["demo.groups"]; w.Gate != "approval" || !strings.Contains(w.Msg, "needs approval: 7 change(s)") {
		t.Errorf("demo.groups waiting event %+v", w)
	}
	if _, err := os.Stat(store); err == nil {
		t.Fatal("up applied changes nobody approved")
	}
	approve := func(args ...string) {
		t.Helper()
		if code, out := box(t, append(append([]string{"approve", "--yes"}, args...), common[:4]...)...); code != ExitOK {
			t.Fatalf("approve %v: %d %s", args, code, out)
		}
	}
	propose := func() {
		t.Helper()
		if code, out := box(t, cmd("plan", "--proposal")...); code != ExitOK || !strings.Contains(out, "proposal:") {
			t.Fatalf("plan --proposal: %d %s", code, out)
		}
	}
	if code, out := box(t, append([]string{"approve", "--all", "--yes"}, common[:4]...)...); code != ExitValidation || !strings.Contains(out, "box plan --proposal") {
		t.Errorf("approve without a proposal: %d %s", code, out)
	}
	propose()
	if _, err := os.Stat(filepath.Join(dir, "proposal.json")); err != nil {
		t.Fatal("no proposal file next to the state")
	}
	// 1b. approving one step applies exactly that step; its dependents wait.
	approve("--step", "demo.groups")
	code, evs = stream("up", "--yes")
	if code != ExitGate {
		t.Fatalf("up after approving one step: exit %d", code)
	}
	if _, ok := of(evs, events.StepDone)["demo.groups"]; !ok {
		t.Error("approved step demo.groups not applied")
	}
	if n := finished(evs).Data["changes"].(float64); n != 7 {
		t.Errorf("applied %v changes, want the 7 approved", n)
	}
	if w := of(evs, events.GateWaiting)["demo.sites"]; w.Gate != "approval" {
		t.Errorf("demo.sites not waiting for approval: %+v", w)
	}
	approve("--all")

	// 2. up without the gate open stops at the gate (exit 3); the rest applies.
	code, evs = stream("up", "--yes")
	if code != ExitGate {
		t.Fatalf("up exit %d, want %d (gate)", code, ExitGate)
	}
	if w := of(evs, events.GateWaiting); w["demo.guarded"].Gate != "G-T" || w["demo.manual"].Gate != "G-M" {
		t.Errorf("gate events %+v", w)
	}
	done := of(evs, events.StepDone)
	for _, s := range []string{"demo.sites", "demo.fragile", "demo.after-fragile", "demo.premium"} {
		if _, ok := done[s]; !ok {
			t.Errorf("%s not done", s)
		}
	}
	if len(of(evs, events.StepProg)) == 0 {
		t.Error("no progress events from the worker")
	}

	// status shows the waiting steps.
	code, out := box(t, append(cmd("status"), "--json")...)
	if code != ExitOK || !strings.Contains(out, `"WAITING"`) {
		t.Errorf("status: %d %s", code, out)
	}

	// gate check reports not passed (exit 3); then open it and confirm the manual gate.
	if code, _ := box(t, cmd("gate", "G-T", "--check")...); code != ExitGate {
		t.Errorf("gate check before opening: exit %d", code)
	}
	if err := os.WriteFile(gateFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := box(t, cmd("gate", "G-T", "--check")...); code != ExitOK || !strings.Contains(out, "passed") {
		t.Errorf("gate check after opening: %d %s", code, out)
	}
	if code, out := box(t, cmd("gate", "G-T", "--confirm")...); code != ExitValidation {
		t.Errorf("confirming a checked gate must be refused: %d %s", code, out)
	}
	if code, _ := box(t, cmd("gate", "G-M", "--confirm")...); code != ExitOK {
		t.Errorf("confirm manual gate: exit %d", code)
	}

	// 3. up resumes: finishes the gated steps without re-applying the rest.
	code, evs = stream("up", "--yes")
	if code != ExitOK {
		t.Fatalf("resume exit %d", code)
	}
	if n := finished(evs).Data["changes"].(float64); n != 2 {
		t.Errorf("resume applied %v changes, want 2 (the two gated steps)", n)
	}

	// 4. a second up changes nothing.
	code, evs = stream("up", "--yes")
	if code != ExitOK || finished(evs).Data["changes"].(float64) != 0 {
		t.Fatalf("idempotent run: exit %d, %v changes", code, finished(evs).Data["changes"])
	}

	// 5. drift: change the tenant by hand, drift reports it (exit 1) and changes nothing; up repairs it.
	var tenant map[string]any
	b, _ := os.ReadFile(store)
	_ = json.Unmarshal(b, &tenant)
	tenant["demo.sites"] = []any{"corporate"}
	b, _ = json.Marshal(tenant)
	_ = os.WriteFile(store, b, 0o600)
	code, evs = stream("drift")
	if code != ExitFailure {
		t.Fatalf("drift exit %d", code)
	}
	if _, ok := of(evs, events.StepChanged)["demo.sites"]; !ok {
		t.Error("drift not reported for demo.sites")
	}
	if b2, _ := os.ReadFile(store); string(b2) != string(b) {
		t.Error("drift changed the tenant")
	}
	if code, _ := box(t, cmd("up", "--yes")...); code != ExitGate {
		t.Fatal("drift repair applied without approval")
	}
	propose()
	approve("--step", "demo.sites")
	if code, out := box(t, cmd("up", "--yes")...); code != ExitOK {
		t.Fatalf("repair failed: %s", out)
	}
	if code, _ := box(t, cmd("drift")...); code != ExitOK {
		t.Error("drift after repair")
	}

	// 6. check one step.
	if code, _ := box(t, cmd("check", "demo.sites")...); code != ExitOK {
		t.Error("check demo.sites failed")
	}

	// 7. a failing step blocks its dependent and fails the run; other steps still run.
	_ = os.Remove(store)
	t.Setenv("BOX_PROVIDER_DEMO_FAIL", "1")
	propose()
	approve("--all")
	code, evs = stream("up", "--yes")
	if code != ExitFailure {
		t.Fatalf("failing run exit %d", code)
	}
	if _, ok := of(evs, events.StepFailed)["demo.fragile"]; !ok {
		t.Error("demo.fragile did not fail")
	}
	if s := of(evs, events.StepSkipped)["demo.after-fragile"]; !strings.Contains(s.Msg, "dependency demo.fragile") {
		t.Errorf("dependent not blocked: %+v", s)
	}
	if _, ok := of(evs, events.StepDone)["demo.sites"]; !ok {
		t.Error("independent steps did not run")
	}
	t.Setenv("BOX_PROVIDER_DEMO_FAIL", "")

	// 8. --only and capabilities fallback.
	code, evs = stream("plan", "--only", "demo.sites", "--capabilities", "exchange")
	if code != ExitOK || len(of(evs, events.StepPlanned)) != 2 {
		t.Errorf("--only: exit %d, planned %v", code, len(of(evs, events.StepPlanned)))
	}
	code, evs = stream("plan", "--capabilities", "exchange")
	if _, ok := of(evs, events.StepPlanned)["demo.basic"]; code != ExitOK || !ok {
		t.Error("fallback not used without conditional_access")
	}
}

func TestLiveTenantNeedsFlag(t *testing.T) {
	dir := t.TempDir()
	b, err := os.ReadFile("../../../examples/acme/box-config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(dir, "live.yaml")
	_ = os.WriteFile(live, []byte(strings.Replace(string(b), "environment: test", "environment: live", 1)), 0o600)
	code, out := box(t, "up", "--yes", "--config", live, "--state", filepath.Join(dir, "s.json"),
		"--providers", "testdata/e2e/providers", "--gates", "testdata/e2e/gates.yaml")
	if code != ExitValidation || !strings.Contains(out, "--live") {
		t.Errorf("live without --live: %d %s", code, out)
	}
}

func TestOrchestrationRequiresValidConfig(t *testing.T) {
	code, out := box(t, "plan", "--config", "testdata/invalid-config.yaml", "--state", filepath.Join(t.TempDir(), "s.json"))
	if code != ExitValidation || !strings.Contains(out, "problem(s)") {
		t.Errorf("invalid config: %d %s", code, out)
	}
	code, out = box(t, "plan")
	if code != ExitValidation || !strings.Contains(out, "--config is required") {
		t.Errorf("no config: %d %s", code, out)
	}
}

func TestBacklogCommands(t *testing.T) {
	if code, out := box(t, "backlog", "check"); code != ExitOK {
		t.Errorf("backlog check: %d %s", code, out)
	}
	if code, out := box(t, "backlog", "done", "T13-08"); code != ExitValidation || !strings.Contains(out, "not DONE") {
		t.Errorf("done with open dependencies must be refused: %d %s", code, out)
	}
}
