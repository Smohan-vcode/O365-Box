package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Smohan-vcode/O365-Box/engine/internal/choices"
)

func catalogue(t *testing.T) *choices.Catalogue {
	t.Helper()
	cat, err := choices.Load("../../../defaults/choices.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

func areas(t *testing.T) []Area {
	t.Helper()
	a, err := LoadAreas("../../../defaults/areas.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// sampleInfo stands in for the plan: a few settings per area, devices off
// when the module is off.
func sampleInfo(s Selection) map[string]AreaInfo {
	out := map[string]AreaInfo{
		"sign-in": {Settings: 22, Lines: []string{"Staff need a second sign-in step"}, Details: []string{"ENT-001 Require two-step sign-in"}},
		"email":   {Settings: 30},
		"cloud":   {Off: "you do not use Microsoft Azure."},
	}
	if s.Tier == "enhanced" {
		out["email"] = AreaInfo{Settings: 36}
	}
	if !s.Modules["devices"] {
		out["devices"] = AreaInfo{Off: "you do not run company computers and phones."}
	} else {
		out["devices"] = AreaInfo{Settings: 80, Lines: []string{"Laptops are encrypted"}}
	}
	return out
}

func fullModel(t *testing.T, theme string, extra func(*Options)) *Model {
	t.Helper()
	opt := Options{DraftPath: filepath.Join(t.TempDir(), "d"), Theme: theme, Keys: testKey, Choices: catalogue(t),
		Areas: areas(t), AreaInfo: sampleInfo, Domain: "acme.example"}
	if extra != nil {
		extra(&opt)
	}
	m := New(opt)
	send(m, tea.WindowSizeMsg{Width: 104, Height: 34})
	return m
}

func at(t *testing.T, m *Model, id string) {
	t.Helper()
	if got := m.steps[m.cur].ID(); got != id {
		t.Fatalf("at %s, want %s (status %q)", got, id, m.status)
	}
}

func TestFiveQuestions(t *testing.T) {
	m := fullModel(t, "dark", nil)
	var ids []string
	for _, s := range m.steps {
		ids = append(ids, s.ID())
	}
	if got := strings.Join(ids, " "); got != "start run careful people review build" {
		t.Fatalf("steps: %s", got)
	}
}

func TestRunSwitches(t *testing.T) {
	m := fullModel(t, "dark", nil)
	send(m, key(tea.KeyEnter), key(tea.KeyEnter), key(tea.KeyTab))
	at(t, m, "run")
	send(m, key(tea.KeySpace))                                                       // computers and phones off
	send(m, key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyDown), key(tea.KeySpace)) // sales on
	send(m, key(tea.KeyDown), key(tea.KeyDown), key(tea.KeySpace))                   // Azure on
	send(m, key(tea.KeyDown), key(tea.KeySpace))                                     // own servers on
	if m.d.Answers["modules.devices"] != false || m.d.Answers["modules.sales"] != true {
		t.Errorf("modules: %v", m.d.Answers)
	}
	if m.d.String("choices.azure.subscription") != "create" || m.d.String("choices.directory.mode") != "hybrid" {
		t.Errorf("choices: %v", m.d.Answers)
	}
	send(m, key(tea.KeyTab))
	at(t, m, "careful")
	if m.d.Answers["modules.records"] != true || m.d.Answers["modules.legal"] != false {
		t.Errorf("switches not written explicitly: %v", m.d.Answers)
	}
	// The dial leaves device choices out when the module is off, and
	// keeps the answers "What you run" gave.
	send(m, key(tea.KeyTab))
	if _, ok := m.d.Answers["choices.devices.windows_minimum"]; ok {
		t.Error("device choice answered with the devices module off")
	}
	if m.d.String("choices.directory.mode") != "hybrid" || m.d.String("careful") != "recommended" || m.d.String("policy.tier") != "core" {
		t.Errorf("after the dial: %v", m.d.Answers)
	}
}

func TestCarefulDial(t *testing.T) {
	m := fullModel(t, "dark", nil)
	send(m, key(tea.KeyEnter), key(tea.KeyEnter), key(tea.KeyTab), key(tea.KeyTab))
	at(t, m, "careful")
	c := m.steps[m.cur].(*carefulScreen)
	if c.cursor != 1 || !strings.Contains(m.View(), "★ our suggestion") {
		t.Fatalf("recommended not preselected: cursor %d", c.cursor)
	}
	send(m, key(tea.KeyRight), key(tea.KeyEnter))
	if m.d.String("careful") != "strict" || m.d.String("policy.tier") != "enhanced" ||
		m.d.String("choices.signin.device_code") != "block" || m.d.String("choices.devices.android_personal_passcode") != "high" {
		t.Errorf("strict: %v", m.d.Answers)
	}
	send(m, key(tea.KeyLeft), key(tea.KeyLeft), key(tea.KeyEnter))
	if m.d.String("careful") != "essential" || m.d.String("policy.tier") != "core" ||
		m.d.String("choices.signin.device_code") != "allow" || m.d.String("choices.session.idle_timeout") != "unmanaged_only" {
		t.Errorf("essential: %v", m.d.Answers)
	}
	if !strings.Contains(m.View(), "Compared with our recommendation") {
		t.Error("no differences shown for essential")
	}
	// Fine-tune opens the individual choices, starting from the dial.
	send(m, key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyEnter))
	if c.tuning == nil || !strings.Contains(m.View(), "Fine-tune") || !strings.Contains(m.View(), "question 1 of") {
		t.Fatalf("fine-tune not open:\n%s", m.View())
	}
	first := c.tuning.questions[0]
	send(m, key(tea.KeyDown), key(tea.KeyEnter))
	if m.d.String(key4(first.ID)) != first.Options[1].Value || m.d.Answers["careful.tuned"] != true {
		t.Errorf("fine-tune answer: %v", m.d.Answers)
	}
	send(m, key(tea.KeyEsc))
	if c.tuning != nil || !strings.Contains(m.View(), "Let me fine-tune") {
		t.Error("esc did not return to the dial")
	}
	send(m, key(tea.KeyTab))
	at(t, m, "people")
	if m.d.String(key4(first.ID)) != first.Options[1].Value {
		t.Error("moving on overwrote a fine-tuned answer")
	}
}

func TestDialSurvivesResume(t *testing.T) {
	m := fullModel(t, "dark", nil)
	path := m.store.Path
	send(m, key(tea.KeyEnter), key(tea.KeyEnter), key(tea.KeyTab), key(tea.KeyTab), key(tea.KeyRight), key(tea.KeyEnter), key(tea.KeyCtrlS))
	m2 := New(Options{DraftPath: path, Theme: "dark", Keys: testKey, Choices: catalogue(t)})
	send(m2, tea.WindowSizeMsg{Width: 104, Height: 34})
	at(t, m2, "careful")
	if c := m2.steps[m2.cur].(*carefulScreen); c.cursor != 2 {
		t.Errorf("dial not restored: cursor %d", c.cursor)
	}
}

func TestReviewApprovesByArea(t *testing.T) {
	var calls []string
	m := fullModel(t, "dark", func(o *Options) {
		o.Approve = func(area, decision string) error { calls = append(calls, area+"="+decision); return nil }
	})
	send(m, key(tea.KeyEnter), key(tea.KeyEnter), key(tea.KeyTab), key(tea.KeyTab), key(tea.KeyTab), key(tea.KeyTab))
	at(t, m, "review")
	v := m.View()
	for _, want := range []string{"Sign-in and accounts", "22 settings", "Staff need a second sign-in step", "Cost: none", "a Approve", "Show details", "Not now"} {
		if !strings.Contains(stripANSI(v), want) {
			t.Errorf("review lacks %q:\n%s", want, v)
		}
	}
	send(m, key(tea.KeyTab))
	at(t, m, "review") // nothing approved yet
	send(m, runes("d"))
	if !strings.Contains(m.View(), "ENT-001") {
		t.Error("details not shown")
	}
	send(m, runes("a")) // sign-in approved, moves to email
	send(m, runes("n")) // email not now
	if m.d.String("approval.sign-in") != Approved || m.d.String("approval.email") != NotNow {
		t.Fatalf("decisions: %v", m.d.Answers)
	}
	send(m, runes("A"))
	if m.d.String("approval.files") != Approved || m.d.String("approval.email") != NotNow {
		t.Errorf("approve all: %v", m.d.Answers)
	}
	if _, ok := m.d.Answers["approval.cloud"]; ok {
		t.Error("an area that is off was approved")
	}
	send(m, runes("wrong.example"), key(tea.KeyEnter))
	if m.d.Answers["review.confirmed"] == true {
		t.Fatal("confirmed with the wrong domain")
	}
	rs := m.steps[m.cur].(*reviewScreen)
	rs.input.Reset()
	send(m, runes("acme.example"), key(tea.KeyEnter), key(tea.KeyTab))
	at(t, m, "build")
	if len(calls) < 3 || calls[0] != "sign-in=approved" || calls[1] != "email=not-now" {
		t.Errorf("approve hook calls: %v", calls)
	}
	if got := Approvals(m.d); got["files"] != Approved || got["email"] != NotNow {
		t.Errorf("Approvals: %v", got)
	}
}

func TestReviewEmptyState(t *testing.T) {
	m := New(Options{DraftPath: filepath.Join(t.TempDir(), "d"), Theme: "plain", Keys: testKey})
	send(m, tea.WindowSizeMsg{Width: 80, Height: 24}, key(tea.KeyEnter), key(tea.KeyEnter))
	for m.steps[m.cur].ID() != "review" {
		send(m, key(tea.KeyTab))
	}
	if !strings.Contains(m.View(), "nothing to review") {
		t.Errorf("no empty state:\n%s", m.View())
	}
}

func TestWelcomeAuditPath(t *testing.T) {
	m := New(Options{DraftPath: filepath.Join(t.TempDir(), "d"), Theme: "dark", Keys: testKey})
	send(m, tea.WindowSizeMsg{Width: 104, Height: 34})
	v := m.View()
	if !strings.Contains(v, "Set up and protect my company") || !strings.Contains(v, "Prepare for an audit") {
		t.Fatalf("welcome lacks the two paths:\n%s", v)
	}
	send(m, key(tea.KeyDown), key(tea.KeyEnter))
	if m.phase != phaseAudit || !strings.Contains(m.View(), "read-only") || !strings.Contains(m.View(), "Global Reader") {
		t.Fatalf("audit screen:\n%s", m.View())
	}
	send(m, key(tea.KeyEnter))
	if m.AuditChosen || !strings.Contains(m.View(), "box audit") {
		t.Error("audit without a hook")
	}
	send(m, key(tea.KeyEsc), key(tea.KeyUp), key(tea.KeyEnter))
	if m.phase != phaseWizard {
		t.Error("setup path did not start the wizard")
	}

	ran := false
	a := New(Options{DraftPath: filepath.Join(t.TempDir(), "a"), Theme: "plain", Keys: testKey, Audit: func() { ran = true }})
	send(a, tea.WindowSizeMsg{Width: 80, Height: 24}, key(tea.KeyDown), key(tea.KeyEnter))
	_, cmd := a.Update(key(tea.KeyEnter))
	if !a.AuditChosen || cmd == nil || cmd() != (tea.QuitMsg{}) || ran {
		t.Errorf("audit hook: chosen=%v ran=%v", a.AuditChosen, ran)
	}
	if _, err := os.Stat(a.store.Path); err == nil {
		t.Error("the audit path wrote the draft")
	}
}

func sampleHome() []AreaStatus {
	return []AreaStatus{
		{Area: "sign-in", Status: "protected"},
		{Area: "email", Status: "watching", Note: "Audit ends in 3 days.", CanEnforce: false},
		{Area: "files", Status: "watching", Note: "Audit period clean: ready to enforce.", CanEnforce: true},
		{Area: "devices", Status: "needs-you", Note: "2 laptops are not encrypted yet."},
	}
}

func TestHomeAfterBuild(t *testing.T) {
	var enforced []string
	feed := make(chan BuildEvent, 2)
	m := fullModel(t, "dark", func(o *Options) {
		o.Areas, o.AreaInfo = areas(t), nil
		o.Build = func() <-chan BuildEvent { return feed }
		o.Home = sampleHome
		o.Enforce = func(a string) error { enforced = append(enforced, a); return nil }
	})
	m.phase, m.cur = phaseWizard, len(m.steps)-1
	send(m, key(tea.KeyEnter))
	feed <- BuildEvent{Step: "x", Title: "X", State: "done", Changes: 1}
	close(feed)
	frames(m, 2)
	if !strings.Contains(m.View(), "open your home screen") {
		t.Fatalf("no home hint:\n%s", m.View())
	}
	send(m, key(tea.KeyEnter))
	if m.phase != phaseHome {
		t.Fatal("enter did not open the home screen")
	}
	v := stripANSI(m.View())
	for _, want := range []string{"Sign-in and accounts", "✓ protected", "◐ watching (audit)", "! needs you", "e Enforce"} {
		if !strings.Contains(v, want) {
			t.Errorf("home lacks %q:\n%s", want, v)
		}
	}
	send(m, runes("e")) // sign-in: protected, nothing to enforce
	if len(enforced) != 0 || !strings.Contains(m.View(), "clean audit period") {
		t.Errorf("enforce offered too early: %v", enforced)
	}
	send(m, key(tea.KeyDown), key(tea.KeyDown), runes("e"))
	if len(enforced) != 1 || enforced[0] != "files" {
		t.Errorf("enforced %v", enforced)
	}
}

func TestNewScreensPlainAndFit(t *testing.T) {
	for _, theme := range []string{"dark", "light", "plain"} {
		for _, size := range [][2]int{{80, 24}, {120, 40}, {40, 12}} {
			m := fullModel(t, theme, func(o *Options) {
				o.Home = sampleHome
				o.Requests = func() ([]ExtensionRequest, error) {
					return []ExtensionRequest{{Number: 4, ExtensionID: "aaaabbbbccccddddeeeeffffgggghhhh", Label: "Example Notes", Requester: "staff-1", Reason: "client call notes"}}, nil
				}
			})
			send(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			where := theme + " welcome"
			check := func() {
				t.Helper()
				v := m.View()
				checkFits(t, v, size[0], size[1], where)
				if theme == "plain" && (strings.Contains(v, "\x1b[") || strings.ContainsAny(v, "│─●◉○━◆◇✓◐")) {
					t.Errorf("%s: plain view has colour or symbols:\n%s", where, v)
				}
			}
			check()
			send(m, key(tea.KeyEnter), key(tea.KeyEnter))
			for i := range m.steps {
				where = theme + " " + m.steps[i].ID()
				check()
				if m.steps[i].ID() == "careful" {
					send(m, key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyEnter))
					where += " fine-tune"
					check()
				}
				if m.steps[i].ID() == "review" {
					send(m, runes("d"))
					check()
					send(m, runes("A"), runes("acme.example"), key(tea.KeyEnter))
					check()
				}
				send(m, key(tea.KeyTab))
			}
			at(t, m, "build")
			m.openHome()
			where = theme + " home"
			check()
			m.phase = phaseAudit
			where = theme + " audit"
			check()
		}
	}
}

func TestHomeExtensionRequests(t *testing.T) {
	waiting := []ExtensionRequest{
		{Number: 4, ExtensionID: "aaaabbbbccccddddeeeeffffgggghhhh", Label: "Example Notes", Requester: "staff-1", Reason: "client call notes", AskEveryone: true},
		{Number: 6, ExtensionID: "ppppoooonnnnmmmmllllkkkkjjjjiiii", Label: "Other helper", Requester: "staff-2", Reason: "translations"},
	}
	var decided []RequestDecision
	m := fullModel(t, "dark", func(o *Options) {
		o.Areas, o.AreaInfo = areas(t), nil
		o.Home = sampleHome
		o.Requests = func() ([]ExtensionRequest, error) { return waiting, nil }
		o.DecideRequest = func(d RequestDecision) (string, error) {
			decided = append(decided, d)
			waiting = waiting[1:]
			return "decided", nil
		}
	})
	send(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.openHome()
	v := stripANSI(m.View())
	for _, want := range []string{"Requests waiting for you (2)", "#4 Example Notes", "staff-1 asks: client call notes (asks to install it for everyone)"} {
		if !strings.Contains(v, want) {
			t.Errorf("home lacks %q:\n%s", want, v)
		}
	}
	// On an area the request keys do nothing.
	send(m, runes("a"))
	if len(decided) != 0 {
		t.Fatal("a decided a request from an area row")
	}
	for range sampleHome() {
		send(m, key(tea.KeyDown))
	}
	if m.curRequest() == nil || m.curRequest().Number != 4 {
		t.Fatalf("cursor not on request 4: %d", m.homeCur)
	}
	send(m, runes("i"))
	if len(decided) != 1 || !decided[0].Approve || !decided[0].Everyone || decided[0].Number != 4 {
		t.Fatalf("decided %+v", decided)
	}
	// Turning down needs a reason; esc records nothing.
	m.homeCur = len(m.home)
	send(m, runes("x"), key(tea.KeyEsc))
	if len(decided) != 1 || m.rejecting != 0 {
		t.Fatal("esc recorded a rejection")
	}
	send(m, runes("x"), key(tea.KeyEnter))
	if len(decided) != 1 || !m.isErr {
		t.Fatal("rejected without a reason")
	}
	send(m, runes("not needed"), key(tea.KeyEnter))
	if len(decided) != 2 || decided[1].Approve || decided[1].Reason != "not needed" || decided[1].Number != 6 {
		t.Fatalf("decided %+v", decided)
	}
	if !strings.Contains(stripANSI(m.View()), "Requests waiting for you (0)") {
		t.Errorf("card not refreshed:\n%s", stripANSI(m.View()))
	}
}

// stripANSI removes colour codes so tests can match the text.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
