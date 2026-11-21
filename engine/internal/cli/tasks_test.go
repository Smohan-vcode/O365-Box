package cli

import (
	"testing"
	"time"

	"github.com/Smohan-vcode/O365-Box/engine/internal/config"
	"github.com/Smohan-vcode/O365-Box/engine/internal/docs"
	"github.com/Smohan-vcode/O365-Box/engine/internal/repo"
)

func TestControlTaskInput(t *testing.T) {
	root, err := repo.FindRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	in, err := controlTaskInput(root, time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if in["today"] != "2026-11-21" {
		t.Errorf("today %v", in["today"])
	}
	due := in["kinds"].([]docs.Due)
	got := map[string]docs.Due{}
	for _, d := range due {
		got[d.Kind] = d
	}
	if _, ok := got["incident_register"]; ok {
		t.Error("an on-event kind has no due date")
	}
	if d := got["access_review"]; d.Current.TaskID != "access_review-2026-Q4" || d.Next.TaskID != "access_review-2027-Q1" || d.OwnerRole != "control-owner" {
		t.Errorf("access_review %+v", d)
	}
}

func TestTasksSummaryAndPick(t *testing.T) {
	kinds := []docs.TaskKind{{Kind: "access_review", Frequency: docs.Quarterly}, {Kind: "app_consent_review", Frequency: docs.Monthly},
		{Kind: "pen_test", Frequency: docs.Annually}, {Kind: "incident_register", Frequency: docs.OnEvent}, {Kind: "access", Frequency: docs.Monthly}}
	items := []taskItem{
		{TaskID: "access_review-2026-Q3", DueOn: "2026-09-30", State: "DONE", ClosedAt: "2026-09-20"},
		{TaskID: "access_review-2026-Q4", DueOn: "2026-12-31", State: "OPEN"},
		{TaskID: "app_consent_review-2026-10", DueOn: "2026-10-31", State: "OPEN"},
		{TaskID: "app_consent_review-2026-11", DueOn: "2026-11-30", State: "OPEN"},
		{TaskID: "access-2026-11", DueOn: "2026-11-30", State: "DONE"},
	}
	rows := summarise(kinds, items, "2026-11-21")
	want := map[string]string{"access_review": "due", "app_consent_review": "overdue", "pen_test": "not created", "incident_register": "on event", "access": "done"}
	for _, r := range rows {
		if r.Status != want[r.Kind] {
			t.Errorf("%s: %s, want %s", r.Kind, r.Status, want[r.Kind])
		}
	}
	if rows[0].Tasks != 2 || rows[0].LastDone.TaskID != "access_review-2026-Q3" {
		t.Errorf("access_review row %+v (the longest kind prefix wins over 'access')", rows[0])
	}
	it, err := pickTask(items, kinds, "app_consent_review", "")
	if err != nil || it.TaskID != "app_consent_review-2026-10" {
		t.Errorf("oldest open first: %+v %v", it, err)
	}
	if _, err := pickTask(items, kinds, "access_review", "access_review-2026-Q3"); err == nil {
		t.Error("a DONE task is never closed again")
	}
	if _, err := pickTask(items, kinds, "pen_test", ""); err == nil {
		t.Error("no open task: error")
	}
}

func TestMayClose(t *testing.T) {
	users := []config.User{{ID: "ana", Roles: []string{"control-owner"}}, {ID: "boss", Roles: []string{"owner"}}, {ID: "bob", Roles: []string{"sender"}},
		{ID: "admin-1", BreakGlass: true, Roles: []string{"owner"}}}
	for id, ok := range map[string]bool{"ana": true, "boss": true, "bob": false, "admin-1": false, "nobody": false} {
		if err := mayClose(users, id, "control-owner"); (err == nil) != ok {
			t.Errorf("%s: %v", id, err)
		}
	}
}
