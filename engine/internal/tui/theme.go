// Package tui is the onboarding terminal UI (SPEC §18), built on Bubble
// Tea. T13-01 is the shell: layout, navigation, help, themes, plain mode
// and save-and-resume to the encrypted draft. Later T13 tasks fill in the
// step screens.
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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
	// Fun bits: a colour ramp for titles and bars, cards for options and
	// small badges. Plain mode keeps them empty.
	Ramp           []lipgloss.Color
	Card, CardOn   lipgloss.Style
	Badge, BadgeOK lipgloss.Style
	BarOn, BarOff  string
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
		Cursor: "▸", Checked: "◆", Unchecked: "◇",
		Card:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted).Padding(0, 1),
		CardOn:  lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(accent).Padding(0, 1),
		Badge:   lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("221")).Bold(true).Padding(0, 1),
		BadgeOK: lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(okc).Padding(0, 1),
		BarOn:   "█", BarOff: "░",
	}
}

// Dark is for dark terminal backgrounds.
func Dark() Theme {
	t := base("dark", "252", "244", "86", "203", "78")
	t.Ramp = []lipgloss.Color{"51", "45", "39", "33", "63", "99", "135", "171", "207", "213"}
	return t
}

// Light is for light terminal backgrounds.
func Light() Theme {
	t := base("light", "236", "245", "25", "160", "28")
	t.Ramp = []lipgloss.Color{"25", "26", "27", "61", "97", "133", "169", "162", "161", "160"}
	return t
}

// PlainTheme has no colour and ASCII marks.
func PlainTheme() Theme {
	s := lipgloss.NewStyle()
	return Theme{Name: "plain", Plain: true, Title: s, Step: s, Muted: s, Accent: s, Selected: s, Help: s, Key: s,
		Err: s, OK: s, Border: s, DoneMark: "[done]", CurMark: "[now]", TodoMark: "[ ]",
		Cursor: ">", Checked: "[x]", Unchecked: "[ ]", Card: s, CardOn: s, Badge: s, BadgeOK: s, BarOn: "#", BarOff: "-"}
}

// Gradient colours each rune of s along the theme's ramp, shifted by
// offset (an animation frame). Plain themes return s unchanged.
func (t Theme) Gradient(s string, offset int, bold bool) string {
	if t.Plain || len(t.Ramp) == 0 {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	n := len(rs)
	for i, r := range rs {
		if r == ' ' {
			b.WriteRune(r)
			continue
		}
		idx := (i*len(t.Ramp)/max(n, 1) + offset) % len(t.Ramp)
		st := lipgloss.NewStyle().Foreground(t.Ramp[idx])
		if bold {
			st = st.Bold(true)
		}
		b.WriteString(st.Render(string(r)))
	}
	return b.String()
}

// Bar is a progress bar width cells wide, filled to frac (0..1).
func (t Theme) Bar(width int, frac float64) string {
	if width < 1 {
		return ""
	}
	on := int(frac*float64(width) + 0.5)
	if on > width {
		on = width
	}
	if t.Plain || len(t.Ramp) == 0 {
		return "[" + strings.Repeat(t.BarOn, on) + strings.Repeat(t.BarOff, width-on) + "]"
	}
	var b strings.Builder
	for i := 0; i < width; i++ {
		if i < on {
			b.WriteString(lipgloss.NewStyle().Foreground(t.Ramp[i*len(t.Ramp)/width]).Render(t.BarOn))
		} else {
			b.WriteString(t.Muted.Render(t.BarOff))
		}
	}
	return b.String()
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
