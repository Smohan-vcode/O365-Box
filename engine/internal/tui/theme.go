// Package tui is the onboarding terminal UI (SPEC §18), built on Bubble
// Tea. T13-01 is the shell: layout, navigation, help, themes, plain mode
// and save-and-resume to the encrypted draft. Later T13 tasks fill in the
// step screens.
package tui

import "github.com/charmbracelet/lipgloss"

// Theme holds the styles. Plain mode uses no colour and no box drawing,
// for screen readers and minimal terminals.
type Theme struct {
	Name                         string
	Plain                        bool
	Title, Step, Muted, Accent   lipgloss.Style
	Selected, Help, Key, Err, OK lipgloss.Style
	Border                       lipgloss.Style
	DoneMark, CurMark, TodoMark  string
	Cursor, Checked, Unchecked   string
}

func base(name string, fg, muted, accent, errc, okc lipgloss.Color) Theme {
	return Theme{
		Name:     name,
		Title:    lipgloss.NewStyle().Bold(true).Foreground(accent),
		Step:     lipgloss.NewStyle().Foreground(fg),
		Muted:    lipgloss.NewStyle().Foreground(muted),
		Accent:   lipgloss.NewStyle().Foreground(accent).Bold(true),
		Selected: lipgloss.NewStyle().Foreground(accent).Bold(true),
		Help:     lipgloss.NewStyle().Foreground(fg),
		Key:      lipgloss.NewStyle().Foreground(accent),
		Err:      lipgloss.NewStyle().Foreground(errc).Bold(true),
		OK:       lipgloss.NewStyle().Foreground(okc),
		Border:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted),
		DoneMark: "●", CurMark: "◉", TodoMark: "○",
		Cursor: "▸", Checked: "[x]", Unchecked: "[ ]",
	}
}

// Dark is for dark terminal backgrounds.
func Dark() Theme {
	return base("dark", "252", "244", "86", "203", "78")
}

// Light is for light terminal backgrounds.
func Light() Theme {
	return base("light", "236", "245", "25", "160", "28")
}

// PlainTheme has no colour and ASCII marks.
func PlainTheme() Theme {
	s := lipgloss.NewStyle()
	return Theme{Name: "plain", Plain: true, Title: s, Step: s, Muted: s, Accent: s, Selected: s, Help: s, Key: s,
		Err: s, OK: s, Border: s, DoneMark: "[done]", CurMark: "[now]", TodoMark: "[ ]",
		Cursor: ">", Checked: "[x]", Unchecked: "[ ]"}
}

// ThemeFor picks a theme by name: dark, light, plain or auto.
func ThemeFor(name string) Theme {
	switch name {
	case "dark":
		return Dark()
	case "light":
		return Light()
	case "plain":
		return PlainTheme()
	}
	if lipgloss.HasDarkBackground() {
		return Dark()
	}
	return Light()
}
