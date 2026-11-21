package run

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Smohan-vcode/O365-Box/engine/internal/events"
	"github.com/Smohan-vcode/O365-Box/engine/internal/plan"
	"github.com/Smohan-vcode/O365-Box/engine/internal/state"
)

// approvals is an in-memory approval record keyed by digest.
type approvals struct {
	mu       sync.Mutex
	ok       map[string]bool
	rejected map[string]bool
	applied  []string
}

func newApprovals() *approvals {
	return &approvals{ok: map[string]bool{}, rejected: map[string]bool{}}
}

func (a *approvals) approve(step string) {
	a.ok[Digest(step, Change{Action: "create", Target: step})] = true
}

func (a *approvals) Check(step string, cs []Change) (un, rej []Change) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range cs {
		d := Digest(step, c)
		switch {
		case a.rejected[d]:
			rej = append(rej, c)
		case !a.ok[d]:
			un = append(un, c)
		}
	}
	return un, rej
}

func (a *approvals) Applied(step string, cs []Change, run string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range cs {
		d := Digest(step, c)
		delete(a.ok, d)
		a.applied = append(a.applied, step)
	}
	return nil
}

func TestUnapprovedChangesApplyNothing(t *testing.T) {
	c := catalog(t, chain)
	f := newFake()
	st := openState(t)
	x, rec := executor(t, c, f, st)
	x.Approvals = newApprovals()
	sum, err := x.Run(context.Background(), ModeApply)
	if err != nil || sum.OK() {
		t.Fatalf("summary %+v %v", sum, err)
	}
	if f.count("one:apply")+f.count("two:apply")+f.count("side:apply") != 0 {
		t.Error("an unapproved change was applied")
	}
	if len(sum.NeedsApproval) != 1 || sum.NeedsApproval[0] != "one" || sum.Status[state.Blocked] != 3 {
		t.Errorf("summary %+v", sum)
	}
	if r := st.Get("one"); r == nil || r.Status != state.Waiting || !strings.Contains(r.Reason, "needs approval: 1 change(s): create one") {
		t.Errorf("state = %+v", r)
	}
	w := rec.Of(events.GateWaiting)
	if len(w) != 1 || w[0].Gate != ApprovalGate || w[0].Msg != "needs approval: 1 change(s)" {
		t.Errorf("waiting events %+v", w)
	}
}

func TestApprovedChangesApplyExactly(t *testing.T) {
	c := catalog(t, chain)
	f := newFake()
	st := openState(t)
	a := newApprovals()
	a.approve("one")
	a.approve("side")
	x, _ := executor(t, c, f, st)
	x.Approvals = a
	sum, _ := x.Run(context.Background(), ModeApply)
	if !f.have["one"] || !f.have["side"] || f.have["two"] || f.have["three"] {
		t.Errorf("applied %v, want one and side only", f.have)
	}
	if strings.Join(sum.NeedsApproval, ",") != "two" || sum.Changes != 2 {
		t.Errorf("summary %+v", sum)
	}
	if strings.Join(a.applied, ",") == "" || len(a.ok) != 0 {
		t.Errorf("applied approvals not consumed: %v %v", a.applied, a.ok)
	}
	// An approval is used once: the same change planned again needs a new one.
	delete(f.have, "one")
	x2, _ := executor(t, c, f, st)
	x2.Approvals = a
	sum, _ = x2.Run(context.Background(), ModeApply)
	if strings.Join(sum.NeedsApproval, ",") != "one" {
		t.Errorf("re-planned change applied on an old approval: %+v", sum)
	}
}

func TestRejectedChangeIsAnException(t *testing.T) {
	c := catalog(t, chain)
	f := newFake()
	st := openState(t)
	a := newApprovals()
	a.approve("one")
	a.rejected[Digest("side", Change{Action: "create", Target: "side"})] = true
	x, rec := executor(t, c, f, st)
	x.Approvals = a
	sum, _ := x.Run(context.Background(), ModeApply)
	if f.have["side"] || !f.have["one"] {
		t.Errorf("applied %v", f.have)
	}
	if strings.Join(sum.Excepted, ",") != "side" {
		t.Errorf("summary %+v", sum)
	}
	if r := st.Get("side"); r == nil || r.Status != state.Skipped || !strings.Contains(r.Reason, "owner exception") {
		t.Errorf("state = %+v", r)
	}
	found := false
	for _, e := range rec.Of(events.StepSkipped) {
		found = found || (e.Step == "side" && e.Data["exception"] == true)
	}
	if !found {
		t.Error("no exception event for side")
	}
}

func TestProposalCollectsPlannedChanges(t *testing.T) {
	c := catalog(t, chain)
	f := newFake()
	f.have["one"] = true
	x, _ := executor(t, c, f, openState(t))
	x.Approvals = newApprovals() // plan mode ignores the gate
	var mu sync.Mutex
	got := map[string]int{}
	x.Proposal = func(s *plan.Step, cs []Change) {
		mu.Lock()
		defer mu.Unlock()
		got[s.ID] = len(cs)
	}
	sum, err := x.Run(context.Background(), ModePlan)
	if err != nil || !sum.OK() || f.count(":apply") != 0 {
		t.Fatalf("plan: %+v %v", sum, err)
	}
	if got["one"] != 0 || got["two"] != 1 || got["three"] != 1 || got["side"] != 1 {
		t.Errorf("proposal = %v", got)
	}
}

func TestDigestCoversDetail(t *testing.T) {
	a := Digest("s", Change{Action: "update", Target: "t", Detail: "x"})
	if a == Digest("s", Change{Action: "update", Target: "t", Detail: "y"}) || a == Digest("s2", Change{Action: "update", Target: "t", Detail: "x"}) {
		t.Error("digest ignores detail or step")
	}
	if len(a) != 64 {
		t.Errorf("digest %q is not a sha256", a)
	}
}
