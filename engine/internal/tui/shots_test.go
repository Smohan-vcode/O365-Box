package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestShots writes ANSI frames of the main screens into $BOX_TUI_SHOTS for
// screenshots (render them with scripts/tui-shots.py). Skipped unless the
// variable is set. The build frames replay a scripted event stream of the
// engine's real steps, and the Review counts and home statuses are samples;
// no tenant is touched.
func TestShots(t *testing.T) {
	dir := os.Getenv("BOX_TUI_SHOTS")
	if dir == "" {
		t.Skip("set BOX_TUI_SHOTS to a directory to write frames")
	}
	lipgloss.SetColorProfile(termenv.ANSI256)
	lipgloss.SetHasDarkBackground(true)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, m *Model) {
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(m.View()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	feed := make(chan BuildEvent, 200)
	m := fullModel(t, "dark", func(o *Options) {
		o.Build = func() <-chan BuildEvent { return feed }
		o.AreaInfo = shotInfo
		o.Home = shotHome
	})
	frames(m, 6)
	write("01-welcome", m)
	send(m, key(tea.KeyDown))
	write("02-welcome-audit", m)
	send(m, key(tea.KeyEnter))
	write("03-audit", m)
	send(m, key(tea.KeyEsc), key(tea.KeyUp), key(tea.KeyEnter))
	write("04-sign-in", m)
	send(m, key(tea.KeyEnter), key(tea.KeyTab), key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyDown))
	write("05-what-you-run", m)
	send(m, key(tea.KeyTab))
	write("06-how-careful", m)
	send(m, key(tea.KeyRight))
	write("07-how-careful-strict", m)
	send(m, key(tea.KeyRight), key(tea.KeyEnter), runes("]"), runes("]"), key(tea.KeyDown))
	write("08-fine-tune", m)
	send(m, key(tea.KeyEsc), key(tea.KeyUp), key(tea.KeyEnter), key(tea.KeyTab), key(tea.KeyTab))
	send(m, runes("a"), runes("n"), key(tea.KeyUp), key(tea.KeyUp), runes("d"))
	write("09-review", m)
	send(m, runes("A"), runes("acme.example"), key(tea.KeyEnter))
	write("10-review-confirmed", m)
	send(m, key(tea.KeyTab))
	write("11-build-ready", m)

	steps := []struct {
		id, title string
		changes   int
	}{
		{"identity.break-glass", "Emergency accounts", 0}, {"identity.users", "People and licences", 6},
		{"identity.groups", "Access and role groups", 13}, {"identity.auth-methods", "Sign-in methods", 3},
		{"identity.directory-settings", "Directory settings", 4}, {"identity.conditional-access", "Conditional Access (report-only)", 5},
		{"exchange.shared-mailboxes", "Shared mailboxes", 6}, {"exchange.lists", "Distribution lists", 3},
		{"exchange.auto-replies", "Automatic replies", 2}, {"sharepoint.sites", "Hub and sites", 10},
		{"sharepoint.libraries", "Libraries and folders", 27}, {"sharepoint.register", "Company registers", 35},
		{"policy.entra", "Identity policies", 0}, {"policy.exchange", "Mail protection", 0},
	}
	send(m, key(tea.KeyEnter))
	for i, s := range steps {
		state := "queued"
		if i < 9 {
			state = "done"
		} else if i < 11 {
			state = "running"
		}
		ch := 0
		if state == "done" {
			ch = s.changes
		}
		feed <- BuildEvent{Step: s.id, Title: s.title, State: state, Changes: ch, Detail: "created library Contracts/Templates"}
	}
	frames(m, 3)
	write("12-build-running", m)
	for _, s := range steps {
		feed <- BuildEvent{Step: s.id, State: "done", Changes: s.changes}
	}
	close(feed)
	frames(m, 4)
	write("13-build-done", m)
	send(m, key(tea.KeyEnter), key(tea.KeyDown), key(tea.KeyDown))
	write("14-home", m)

	l := fullModel(t, "light", nil)
	send(l, key(tea.KeyEnter), key(tea.KeyEnter), key(tea.KeyTab), key(tea.KeyTab))
	write("15-how-careful-light", l)
	p := fullModel(t, "plain", func(o *Options) { o.AreaInfo = shotInfo })
	send(p, key(tea.KeyEnter), key(tea.KeyEnter), key(tea.KeyTab), key(tea.KeyTab), key(tea.KeyTab), key(tea.KeyTab))
	write("16-review-plain", p)
}

// shotInfo is a sample plan by area for the screenshots.
func shotInfo(s Selection) map[string]AreaInfo {
	n := func(core, enhanced int) int {
		if s.Tier == "enhanced" {
			return core + enhanced
		}
		return core
	}
	return map[string]AreaInfo{
		"sign-in": {Settings: n(38, 11), Lines: []string{"Copilot and AI features: Off until our AI policy approves them", "Allow sign-in with a code on another device? Allow for now"},
			Details: []string{"ENT-001 Conditional Access: require multi-factor authentication, all users, all apps",
				"ENT-002 Conditional Access: block legacy authentication clients",
				"ENT-003 Conditional Access: phishing-resistant authentication strength for administrator roles"}},
		"email":          {Settings: n(32, 9), Lines: []string{"Suspicious email attachments: Hold the email until the attachment is checked"}},
		"files":          {Settings: n(15, 3)},
		"meetings":       {Settings: n(11, 3)},
		"devices":        {Settings: n(118, 28), Lines: []string{"Oldest Windows version allowed on company PCs: Windows 11 version 23H2 or later"}},
		"sensitive-info": {Settings: n(25, 6)},
		"apps":           {Settings: n(28, 6)},
		"web-address":    {Settings: 0, Lines: []string{"A short guided checklist: registrar lock, two-step sign-in, auto-renew"}},
		"cloud":          {Off: "you do not use Microsoft Azure. Switch it on under What you run."},
	}
}

func shotHome() []AreaStatus {
	return []AreaStatus{
		{Area: "sign-in", Status: "protected", Note: "Enforced. Last checked today."},
		{Area: "email", Status: "protected", Note: "Enforced. Last checked today."},
		{Area: "files", Status: "watching", Note: "Audit period clean: nobody would have been blocked. Ready to enforce.", CanEnforce: true},
		{Area: "meetings", Status: "watching", Note: "In audit for 6 more days."},
		{Area: "devices", Status: "needs-you", Note: "2 laptops are not encrypted yet."},
		{Area: "sensitive-info", Status: "watching", Note: "In audit for 9 more days."},
		{Area: "apps", Status: "protected"},
		{Area: "web-address", Status: "needs-you", Note: "Finish the registrar checklist."},
	}
}

func frames(m *Model, n int) {
	for i := 0; i < n; i++ {
		m.Update(tickMsg{})
	}
}
