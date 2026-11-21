package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Smohan-vcode/O365-Box/engine/internal/approval"
)

// TestApprovalGate drives box plan --proposal, box approve and box up
// against the demo provider (SPEC §25.1, T0-08).
func TestApprovalGate(t *testing.T) {
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh not installed")
	}
	dir := t.TempDir()
	store := filepath.Join(dir, "tenant.json")
	t.Setenv("BOX_PROVIDER_DEMO_STORE", store)
	t.Setenv("BOX_PROVIDER_DEMO_GATE", filepath.Join(dir, "gate-open"))
	t.Setenv("BOX_PROVIDER_DEMO_FAIL", "")
	base := []string{"--config", "../../../examples/acme/box-config.yaml", "--state", filepath.Join(dir, "state.json")}
	run := append(append([]string{}, base...), "--providers", "testdata/e2e/providers", "--gates", "testdata/e2e/gates.yaml")
	tenant := func() map[string]any {
		m := map[string]any{}
		if b, err := os.ReadFile(store); err == nil {
			_ = json.Unmarshal(b, &m)
		}
		return m
	}

	code, out := box(t, append([]string{"plan", "--proposal"}, run...)...)
	if code != ExitOK || !strings.Contains(out, "Other [other]") || !strings.Contains(out, "Create demo.groups/") {
		t.Fatalf("plan --proposal: %d %s", code, out)
	}
	p, err := approval.ReadProposal(filepath.Join(dir, approval.ProposalFile))
	if err != nil {
		t.Fatal(err)
	}
	digest := func(step string) string {
		for _, c := range p.Changes {
			if c.Step == step {
				return c.Digest
			}
		}
		t.Fatalf("no change for %s in the proposal", step)
		return ""
	}

	// Approving needs the word typed, a reason goes with a rejection.
	if code, out := boxIn(t, "yes\n", append([]string{"approve", "--change", digest("demo.premium")[:10]}, base...)...); code != ExitValidation || !strings.Contains(out, "not confirmed") {
		t.Errorf("approve without typing approve: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, approval.ApprovalsFile)); err == nil {
		if f, _ := approval.Open(filepath.Join(dir, approval.ApprovalsFile)); len(f.Approvals()) > 0 {
			t.Error("an unconfirmed approval was recorded")
		}
	}
	if code, out := box(t, append([]string{"approve", "--reject", digest("demo.premium")[:10], "--yes"}, base...)...); code != ExitValidation || !strings.Contains(out, "--reason") {
		t.Errorf("reject without a reason: %d %s", code, out)
	}
	if code, out := box(t, append([]string{"approve", "--area", "nowhere", "--yes"}, base...)...); code != ExitValidation || !strings.Contains(out, "no area") {
		t.Errorf("unknown area: %d %s", code, out)
	}

	// Approve one change: an apply applies exactly that step.
	if code, out := boxIn(t, "approve\n", append([]string{"approve", "--change", digest("demo.premium")[:10]}, base...)...); code != ExitOK || !strings.Contains(out, "approved 1 change(s)") {
		t.Fatalf("approve one change: %d %s", code, out)
	}
	code, out = box(t, append([]string{"up", "--yes"}, run...)...)
	if code != ExitGate || !strings.Contains(out, "needs approval: demo.") || !strings.Contains(out, "demo.groups: needs approval: 7 change(s)") {
		t.Errorf("up with unapproved changes: %d %s", code, out)
	}
	if got := tenant(); len(got) != 1 || got["demo.premium"] == nil {
		t.Errorf("tenant after approving one change = %v, want only demo.premium", got)
	}
	if b, err := os.ReadFile(filepath.Join(dir, approval.EventsFile)); err != nil || !strings.Contains(string(b), `"action":"approval"`) {
		t.Errorf("no approval Events item: %v %s", err, b)
	}
	f, _ := approval.Open(filepath.Join(dir, approval.ApprovalsFile))
	if a := f.Approvals(); len(a) != 1 || a[0].AppliedAt == nil || a[0].By == "" {
		t.Errorf("approval history = %+v", a)
	}

	// Reject one change of demo.groups: an exception with the reason; the step applies nothing.
	if code, out := boxIn(t, "reject\n", append([]string{"approve", "--reject", digest("demo.groups")[:10], "--reason", "not wanted"}, base...)...); code != ExitOK || !strings.Contains(out, "rejected 1 change(s)") {
		t.Fatalf("reject: %d %s", code, out)
	}
	f, _ = approval.Open(filepath.Join(dir, approval.ApprovalsFile))
	if ex := f.Exceptions(); len(ex) != 1 || ex[0].Reason != "not wanted" || ex[0].Step != "demo.groups" {
		t.Errorf("exceptions = %+v", ex)
	}
	// Approve the area: every other change of it, the rejected one stays an exception.
	if code, out := box(t, append([]string{"approve", "--area", "other", "--yes"}, base...)...); code != ExitOK {
		t.Fatalf("approve area: %d %s", code, out)
	}
	f, _ = approval.Open(filepath.Join(dir, approval.ApprovalsFile))
	if ex := f.Exceptions(); len(ex) != 1 {
		t.Errorf("approving an area must not lift a rejection")
	}
	code, out = box(t, append([]string{"up", "--yes"}, run...)...)
	if !strings.Contains(out, "owner exception: demo.groups") {
		t.Errorf("summary does not name the exception: %d %s", code, out)
	}
	if got := tenant(); got["demo.groups"] != nil || got["demo.sites"] == nil {
		t.Errorf("tenant after rejecting a demo.groups change = %v", got)
	}
}
