package tui

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Smohan-vcode/O365-Box/engine/internal/draft"
)

var testKey = draft.Static(bytes.Repeat([]byte{3}, 32))

func key(k tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: k} }
func runes(s string) tea.KeyMsg    { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func send(m *Model, msgs ...tea.Msg) {
	for _, msg := range msgs {
		m.Update(msg)
	}
}

func newModel(t *testing.T, path, theme string) *Model {
	t.Helper()
	m := New(Options{DraftPath: path, Theme: theme, Keys: testKey})
	send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

func checkFits(t *testing.T, view string, w, h int, where string) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) > h {
		t.Errorf("%s: %d lines, max %d", where, len(lines), h)
	}
	for i, l := range lines {
		if lw := lipgloss.Width(l); lw > w {
			t.Errorf("%s: line %d is %d cells wide, max %d: %q", where, i+1, lw, w, l)
		}
	}
}

func TestEveryScreenFits(t *testing.T) {
	for _, theme := range []string{"dark", "light", "plain"} {
		for _, size := range [][2]int{{80, 24}, {120, 40}, {40, 12}} {
			m := New(Options{DraftPath: filepath.Join(t.TempDir(), "d"), Theme: theme, Keys: testKey})
			send(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]}, key(tea.KeyEnter))
			for i := range m.steps {
				checkFits(t, m.View(), size[0], size[1], theme+" "+m.steps[i].ID())
				if !strings.Contains(m.View(), m.steps[i].Title()) {
					t.Errorf("%s: title missing", m.steps[i].ID())
				}
				send(m, key(tea.KeyTab))
			}
			if m.cur != len(m.steps)-1 {
				t.Errorf("%s %v: stopped at %d", theme, size, m.cur)
			}
		}
	}
}

func TestStartMustBeChosen(t *testing.T) {
	m := newModel(t, filepath.Join(t.TempDir(), "d"), "dark")
	send(m, key(tea.KeyTab))
	if m.cur != 0 || !m.isErr || !strings.Contains(m.View(), "choose one option") {
		t.Fatalf("moved on without a choice: cur=%d status=%q", m.cur, m.status)
	}
	send(m, key(tea.KeyDown), key(tea.KeySpace), key(tea.KeyTab))
	if m.cur != 1 || m.d.String("start.mode") != "signup" {
		t.Errorf("cur=%d mode=%q", m.cur, m.d.String("start.mode"))
	}
}

func TestQuitAndResumeAtSameScreen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d")
	m := newModel(t, path, "dark")
	send(m, key(tea.KeyEnter), key(tea.KeyTab), key(tea.KeyTab))
	_, cmd := m.Update(key(tea.KeyCtrlS))
	if cmd == nil || !m.Saved {
		t.Fatal("ctrl+s did not save and quit")
	}
	if msg := cmd(); msg != (tea.QuitMsg{}) {
		t.Errorf("want quit, got %T", msg)
	}
	m2 := newModel(t, path, "dark")
	if m2.steps[m2.cur].ID() != "addresses" || m2.d.String("start.mode") != "existing" {
		t.Errorf("resumed at %s with %v", m2.steps[m2.cur].ID(), m2.d.Answers)
	}
	if !strings.Contains(m2.View(), "Welcome back") {
		t.Error("no welcome-back message")
	}
	send(m2, key(tea.KeyShiftTab), key(tea.KeyShiftTab))
	if m2.cur != 0 || !strings.Contains(m2.View(), "[x] Yes, sign me in") {
		t.Errorf("start choice not restored on screen:\n%s", m2.View())
	}
}

func TestCtrlCAlsoSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d")
	m := newModel(t, path, "dark")
	send(m, key(tea.KeyEnter), key(tea.KeyTab), key(tea.KeyCtrlC))
	if m2 := newModel(t, path, "dark"); m2.steps[m2.cur].ID() != "company" {
		t.Errorf("resumed at %s", m2.steps[m2.cur].ID())
	}
}

func TestPassphraseWhenNoKeychain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d")
	noKeychain := func() bool { return false }
	m := New(Options{DraftPath: path, Theme: "plain", Keychain: noKeychain})
	send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.phase != phaseUnlock || !strings.Contains(m.View(), "choose a passphrase") {
		t.Fatalf("no passphrase prompt:\n%s", m.View())
	}
	send(m, runes("short"), key(tea.KeyEnter))
	if m.phase != phaseUnlock || !strings.Contains(m.View(), "at least 8") {
		t.Fatal("short passphrase accepted")
	}
	send(m, runes("correct horse battery"), key(tea.KeyEnter))
	if m.phase != phaseWizard {
		t.Fatalf("not unlocked: %q", m.status)
	}
	if strings.Contains(m.View(), "correct horse") {
		t.Error("passphrase shown on screen")
	}
	send(m, key(tea.KeyEnter), key(tea.KeyTab), key(tea.KeyCtrlS))

	m2 := New(Options{DraftPath: path, Theme: "plain", Keychain: noKeychain})
	send(m2, tea.WindowSizeMsg{Width: 80, Height: 24}, runes("wrong passphrase"), key(tea.KeyEnter))
	if m2.phase != phaseUnlock || !strings.Contains(m2.View(), "does not open") {
		t.Fatalf("wrong passphrase: phase=%v %q", m2.phase, m2.status)
	}
	send(m2, runes("correct horse battery"), key(tea.KeyEnter))
	if m2.phase != phaseWizard || m2.steps[m2.cur].ID() != "company" {
		t.Errorf("resume with passphrase: phase=%v at %s", m2.phase, m2.steps[m2.cur].ID())
	}
	// A keychain appearing later must not hide a passphrase draft.
	m3 := New(Options{DraftPath: path, Theme: "plain", Keychain: func() bool { return true }})
	if m3.phase != phaseUnlock {
		t.Error("passphrase draft opened without its passphrase")
	}
}

func TestSignalsAreNotColourOnly(t *testing.T) {
	m := newModel(t, filepath.Join(t.TempDir(), "d"), "dark")
	send(m, key(tea.KeyEnter), key(tea.KeyTab))
	v := m.View()
	for _, want := range []string{"● Start", "◉ Company", "○ Addresses", "Step 2 of 8"} {
		if !strings.Contains(v, want) {
			t.Errorf("dark view lacks %q", want)
		}
	}
	p := newModel(t, filepath.Join(t.TempDir(), "d"), "plain")
	send(p, key(tea.KeyEnter), key(tea.KeyTab))
	pv := p.View()
	if strings.Contains(pv, "\x1b[") || strings.ContainsAny(pv, "│─●◉○") {
		t.Errorf("plain view has colour or box drawing:\n%s", pv)
	}
	if !strings.Contains(pv, "[now] Company") || !strings.Contains(pv, "[done] Start") || !strings.Contains(pv, "What is this?") {
		t.Errorf("plain view lacks text markers or help:\n%s", pv)
	}
}

func TestHelpAndThemeToggle(t *testing.T) {
	m := newModel(t, filepath.Join(t.TempDir(), "d"), "dark")
	if !strings.Contains(m.View(), "What is this?") {
		t.Error("help pane hidden at 80 columns")
	}
	send(m, runes("?"))
	if strings.Contains(m.View(), "What is this?") {
		t.Error("? did not hide help")
	}
	send(m, key(tea.KeyCtrlT))
	if m.th.Name != "light" {
		t.Errorf("theme %s", m.th.Name)
	}
}
