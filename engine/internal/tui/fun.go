package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// tickMsg drives the small animations: the welcome shimmer, spinners on
// the Build screen and the confetti when it finishes.
type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(110*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

var logo = []string{
	`    ╭───────────────╮  `,
	`   ╱               ╱│  `,
	`  ╭───────────────╮ │  `,
	`  │  ▣   ▣   ▣    │ │  `,
	`  │  ▣   ▣   ▣    │ │  `,
	`  │  ▣   ▣   ▣    │╱   `,
	`  ╰───────────────╯    `,
}

// spinner frames for work in progress.
var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// welcomeCentered is the first screen of a new onboarding: the logo and
// the text, each centred on its own.
func (m *Model) welcomeCentered(w int) string {
	th := m.th
	var top strings.Builder
	if th.Plain {
		top.WriteString("COMPANY IN A BOX")
	} else {
		for i, l := range logo {
			top.WriteString(th.Gradient(strings.TrimRight(l, " "), m.frame/2+i, true) + "\n")
		}
		top.WriteString("\n" + th.Gradient("C O M P A N Y   I N   A   B O X", m.frame/2, true))
	}
	tw := min(w, 76)
	var b strings.Builder
	b.WriteString(wrap("Your whole company on Microsoft 365, set up and secured in one window.", tw) + "\n\n")
	steps := []struct{ icon, text string }{
		{"①", "Tell us about your company, your people and how strict to be."},
		{"②", "Review the full plan. Nothing changes until you say so."},
		{"③", "Watch it build: mailboxes, sites, groups, policies. Coffee optional."},
	}
	if th.Plain {
		steps[0].icon, steps[1].icon, steps[2].icon = "1.", "2.", "3."
	}
	for _, s := range steps {
		b.WriteString(wrapHang(th.Accent.Render(s.icon)+" "+s.text, tw, "", "   ") + "\n")
	}
	b.WriteString("\n" + th.Muted.Render(wrap("About 20 minutes. Your answers are saved as you go, encrypted on this computer.", tw)))
	cta := "Press enter to begin"
	if !th.Plain {
		cta = th.CardOn.Render(th.Gradient("▶  Press enter to begin", m.frame, true))
	}
	return center(top.String(), w) + "\n\n" + center(b.String(), w) + "\n\n" + center(cta, w)
}

// center indents a block so its widest line sits in the middle.
func center(block string, w int) string {
	lines := strings.Split(block, "\n")
	widest := 0
	for _, l := range lines {
		widest = max(widest, lipgloss.Width(l))
	}
	pad := (w - widest) / 2
	if pad <= 0 {
		return block
	}
	p := strings.Repeat(" ", pad)
	for i, l := range lines {
		if l != "" {
			lines[i] = p + l
		}
	}
	return strings.Join(lines, "\n")
}

// confetti is a deterministic sprinkle for the finish line.
func confetti(th Theme, frame, w, rows int) string {
	if th.Plain || len(th.Ramp) == 0 {
		return ""
	}
	bits := []string{"✦", "•", "◆", "✧", "▪", "★", "·"}
	var b strings.Builder
	seed := uint32(frame*7919 + 17)
	for r := 0; r < rows; r++ {
		for c := 0; c < w; c++ {
			seed = seed*1664525 + 1013904223
			if seed>>27 < 3 {
				col := th.Ramp[int(seed>>8)%len(th.Ramp)]
				b.WriteString(lipgloss.NewStyle().Foreground(col).Render(bits[int(seed>>16)%len(bits)]))
			} else {
				b.WriteByte(' ')
			}
		}
		if r < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
