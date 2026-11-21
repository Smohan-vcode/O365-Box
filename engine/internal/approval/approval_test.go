package approval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/plan"
	"github.com/Smohan-vcode/O365-Box/engine/internal/run"
)

func repoAreas(t *testing.T) *Areas {
	t.Helper()
	a, err := LoadAreas("../../../defaults/areas.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// Every step the providers declare belongs to one of the nine areas.
func TestAreasCoverEveryStep(t *testing.T) {
	a := repoAreas(t)
	want := []string{"sign-in", "email", "files", "meetings", "devices", "sensitive-info", "apps", "web-address", "cloud"}
	if len(a.Areas) != len(want) {
		t.Fatalf("%d areas, want %d", len(a.Areas), len(want))
	}
	for i, id := range want {
		if a.Areas[i].ID != id {
			t.Errorf("area %d is %q, want %q", i, a.Areas[i].ID, id)
		}
	}
	c, err := plan.Load("../../providers", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Steps) == 0 {
		t.Fatal("no steps")
	}
	for id := range c.Steps {
		if a.Of(id) == Other {
			t.Errorf("step %s is in no area: add it to defaults/areas.yaml", id)
		}
	}
	for step, area := range map[string]string{"identity.users": "sign-in", "policy.sharepoint": "files",
		"policy.sharepoint.intune": "devices", "policy.teams.identity": "meetings", "policy.purview.identity": "sensitive-info",
		"policy.intune-windows": "devices", "exchange.lists": "email", "policy.m365-apps": "apps"} {
		if got := a.Of(step); got != area {
			t.Errorf("%s is in %s, want %s", step, got, area)
		}
	}
}

func TestApproveRejectApplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), ApprovalsFile)
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a := repoAreas(t)
	st := &plan.Step{ID: "sharepoint.sites", Title: "Hub and sites"}
	c1 := run.Change{Action: "create", Target: "site/finance"}
	c2 := run.Change{Action: "update", Target: "site/legal", Detail: "sharing off"}
	ch1, ch2 := NewChange(a, st, c1), NewChange(a, st, c2)
	if ch1.Area != "files" || ch2.Text != "Change site/legal: sharing off" {
		t.Errorf("changes %+v %+v", ch1, ch2)
	}
	now := time.Now().UTC()
	if _, err := f.Approve([]Change{ch1}, "owner", "r-1", now); err != nil {
		t.Fatal(err)
	}
	un, rej := f.Check(st.ID, []run.Change{c1, c2})
	if len(un) != 1 || un[0] != c2 || len(rej) != 0 {
		t.Errorf("check: %v %v", un, rej)
	}
	// A different detail is a different change.
	if un, _ := f.Check(st.ID, []run.Change{{Action: "create", Target: "site/finance", Detail: "new"}}); len(un) != 1 {
		t.Error("a change with a new detail passed on an old approval")
	}
	if err := f.Reject([]Change{ch2}, "", "owner", "r-1", now); err == nil {
		t.Error("a rejection without a reason was recorded")
	}
	if err := f.Reject([]Change{ch2}, "legal needs sharing", "owner", "r-1", now); err != nil {
		t.Fatal(err)
	}
	if _, rej := f.Check(st.ID, []run.Change{c2}); len(rej) != 1 {
		t.Error("rejected change not reported")
	}
	if err := f.Applied(st.ID, []run.Change{c1}, "r-2"); err != nil {
		t.Fatal(err)
	}
	if un, _ := f.Check(st.ID, []run.Change{c1}); len(un) != 1 {
		t.Error("an applied approval authorised a second apply")
	}
	g, _ := Open(path)
	ap, ex := g.Approvals(), g.Exceptions()
	if len(ap) != 1 || ap[0].AppliedRun != "r-2" || ap[0].By != "owner" || len(ex) != 1 || ex[0].Reason != "legal needs sharing" {
		t.Errorf("saved %+v %+v", ap, ex)
	}
}

func TestEventItemsAppend(t *testing.T) {
	p := filepath.Join(t.TempDir(), EventsFile)
	for i := 0; i < 2; i++ {
		if err := AppendEvent(p, &EventItem{At: time.Now(), Actor: "owner", Entity: "proposal", EntityID: "r-1", Action: "approval"}); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(p)
	if lines := strings.Count(string(b), "\n"); lines != 2 || !strings.Contains(string(b), `"event_id":"ev-`) {
		t.Errorf("events file %s", b)
	}
}
