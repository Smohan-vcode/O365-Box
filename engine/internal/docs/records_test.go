package docs

import (
	"strings"
	"testing"
	"time"
)

// TestRepositorySchedule checks that every ControlTasks kind the documents
// name is defined in defaults/controls.yaml at the same frequency, and that
// requirements and controls agree.
func TestRepositorySchedule(t *testing.T) {
	kinds, problems, err := LoadSchedule(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Error(p)
	}
	want := map[string]string{
		"access_review": Quarterly, "break_glass_test": Semiannually, "restore_test": Annually, "incident_exercise": Annually,
		"vulnerability_review": Weekly, "alert_review": Daily, "app_consent_review": Monthly, "risk_assessment": Annually,
		"vendor_review": Annually, "awareness": Annually, "policy_review": Annually, "exception_review": Monthly,
		"incident_register": OnEvent,
	}
	got := map[string]TaskKind{}
	for _, k := range kinds {
		got[k.Kind] = k
		if k.OwnerRole == "" {
			t.Errorf("%s has no owner role", k.Kind)
		}
	}
	for k, f := range want {
		if got[k].Frequency != f {
			t.Errorf("%s: frequency %q, want %q", k, got[k].Frequency, f)
		}
	}
	if got["access_review"].OwnerRole != "control-owner" || got["break_glass_test"].OwnerRole != "owner" {
		t.Errorf("owner roles come from the procedure: %+v", got["access_review"])
	}
	if got["access_review"].Prepares != PrepareAccessReview || got["app_consent_review"].Prepares != PrepareAppConsents {
		t.Error("access review and app consent review prepare their artefacts")
	}
	rr := RequirementRecords(kinds)
	if len(rr["IAM-13"]) != 1 || rr["IAM-13"][0].Kind != "access_review" {
		t.Errorf("IAM-13 -> %+v", rr["IAM-13"])
	}
	if len(rr["APP-01"]) != 1 || rr["APP-01"][0].Frequency != Monthly {
		t.Errorf("APP-01 -> %+v", rr["APP-01"])
	}
}

func TestScheduleProblems(t *testing.T) {
	docs := []Doc{
		{ID: "POL-01", Kind: KindPolicy, OwnerRole: "owner", Records: []string{"ControlTasks:access_review quarterly", "ControlTasks:ghost_review monthly"}},
		{ID: "PRC-01", Kind: KindProcedure, OwnerRole: "control-owner", Records: []string{"ControlTasks:access_review monthly", "Evidence:x weekly"}},
	}
	controls := []Control{
		{ID: "C-01", Frequency: Quarterly, Source: "task.access_review", Policy: "POL-01", Procedure: "PRC-01", Requirements: []string{"IAM-13"}},
		{ID: "C-02", Frequency: "fortnightly", Source: "task.unused", Policy: "POL-01", Prepares: "magic"},
	}
	kinds, problems := Schedule(docs, controls)
	all := strings.Join(problems, "\n")
	for _, w := range []string{"ghost_review, which no control", "PRC-01: records ControlTasks:access_review monthly, but C-01 is quarterly",
		`frequency "fortnightly"`, `prepares "magic"`, "unused is named in no document"} {
		if !strings.Contains(all, w) {
			t.Errorf("missing problem %q in:\n%s", w, all)
		}
	}
	if len(kinds) != 2 || kinds[0].Kind != "access_review" || kinds[0].OwnerRole != "control-owner" {
		t.Errorf("kinds %+v", kinds)
	}
	rp := CheckRequirementRecords(kinds, map[string]string{"IAM-13": "task.access_review", "IAM-07": "task.break_glass_test", "APP-01": "collector.x"},
		map[string][]string{"APP-01": {"control-task:access_review"}})
	all = strings.Join(rp, "\n")
	for _, w := range []string{"IAM-07: evidence names task kind break_glass_test", "APP-01: procedures names access_review, but C-01 does not list APP-01"} {
		if !strings.Contains(all, w) {
			t.Errorf("missing %q in:\n%s", w, all)
		}
	}
}

func TestParseRecord(t *testing.T) {
	r := ParseRecord("ControlTasks:break_glass_test every six months")
	if r.List != "ControlTasks" || r.Kind != "break_glass_test" || r.Frequency != Semiannually {
		t.Errorf("%+v", r)
	}
	if r := ParseRecord("ControlTasks:alert_review each working day"); r.Frequency != Daily {
		t.Errorf("%+v", r)
	}
	if r := ParseRecord("Evidence:mail_auth weekly"); r.List != "Evidence" || r.Frequency != "" {
		t.Errorf("%+v", r)
	}
}

func TestPeriods(t *testing.T) {
	sat := time.Date(2026, 11, 21, 15, 0, 0, 0, time.UTC) // a Saturday
	cases := []struct{ freq, id, start, due, next string }{
		{Daily, "2026-11-23", "2026-11-23", "2026-11-23", "2026-11-24"},
		{Weekly, "2026-W47", "2026-11-16", "2026-11-22", "2026-W48"},
		{Monthly, "2026-11", "2026-11-01", "2026-11-30", "2026-12"},
		{Quarterly, "2026-Q4", "2026-10-01", "2026-12-31", "2027-Q1"},
		{Semiannually, "2026-H2", "2026-07-01", "2026-12-31", "2027-H1"},
		{Annually, "2026", "2026-01-01", "2026-12-31", "2027"},
	}
	for _, c := range cases {
		p, err := PeriodOf("k", c.freq, sat)
		if err != nil {
			t.Fatal(err)
		}
		if p.ID != c.id || p.Start != c.start || p.DueOn != c.due || p.TaskID != "k-"+c.id {
			t.Errorf("%s: %+v", c.freq, p)
		}
		n, err := NextPeriod("k", c.freq, p)
		if err != nil || n.ID != c.next {
			t.Errorf("%s next: %+v %v", c.freq, n, err)
		}
	}
	// Friday's daily task is followed by Monday's.
	fri, _ := PeriodOf("k", Daily, time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC))
	if n, _ := NextPeriod("k", Daily, fri); n.ID != "2026-11-30" {
		t.Errorf("after Friday: %+v", n)
	}
	// ISO week 53 and the year boundary.
	if p, _ := PeriodOf("k", Weekly, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)); p.ID != "2026-W53" {
		t.Errorf("1 Jan 2027: %+v", p)
	}
	if _, err := PeriodOf("k", OnEvent, sat); err == nil {
		t.Error("on_event has no periods")
	}
	due, err := DueOn([]TaskKind{{Kind: "a", Frequency: Monthly}, {Kind: "b", Frequency: OnEvent}}, sat)
	if err != nil || len(due) != 1 || due[0].Current.TaskID != "a-2026-11" || due[0].Next.TaskID != "a-2026-12" {
		t.Errorf("%+v %v", due, err)
	}
}
