package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/Smohan-vcode/O365-Box/engine/internal/choices"
)

// TestShots writes ANSI frames of the main screens into $BOX_TUI_SHOTS for
// screenshots (render them with scripts/tui-shots.py). Skipped unless the
// variable is set. The build frames replay a scripted event stream of the
// engine's real steps; no tenant is touched.
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
	cat, err := choices.Load("../../../defaults/choices.yaml")
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, m *Model) {
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(m.View()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	feed := make(chan BuildEvent, 200)
	m := New(Options{DraftPath: filepath.Join(t.TempDir(), "d"), Theme: "dark", Keys: testKey, Choices: cat,
		Build: func() <-chan BuildEvent { return feed }})
	send(m, tea.WindowSizeMsg{Width: 104, Height: 34})
	frames(m, 6)
	write("01-welcome", m)
	send(m, key(tea.KeyEnter))
	write("02-start", m)
	send(m, key(tea.KeyEnter), key(tea.KeyTab), key(tea.KeyTab), key(tea.KeyTab))
	write("03-security", m)
	send(m, runes("]"), runes("]"), key(tea.KeyDown))
	write("04-security-question", m)
	send(m, key(tea.KeyTab))
	send(m, runes("]"), runes("]"), runes("]"))
	write("05-structure", m)
	send(m, key(tea.KeyTab), key(tea.KeyTab), key(tea.KeyTab))
	write("06-build-ready", m)

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
	write("07-build-running", m)
	for _, s := range steps {
		feed <- BuildEvent{Step: s.id, State: "done", Changes: s.changes}
	}
	close(feed)
	frames(m, 4)
	write("08-build-done", m)

	l := New(Options{DraftPath: filepath.Join(t.TempDir(), "l"), Theme: "light", Keys: testKey, Choices: cat})
	send(l, tea.WindowSizeMsg{Width: 104, Height: 34}, key(tea.KeyEnter), key(tea.KeyEnter), key(tea.KeyTab), key(tea.KeyTab), key(tea.KeyTab))
	write("09-security-light", l)
}

func frames(m *Model, n int) {
	for i := 0; i < n; i++ {
		m.Update(tickMsg{})
	}
}
