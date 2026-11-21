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
// the text, each centred on its own. Short terminals skip the logo so the
// two paths stay on screen.
func (m *Model) welcomeCentered(w, h int) string {
	th := m.th
	var top strings.Builder
	if th.Plain {
		top.WriteString("COMPANY IN A BOX")
	} else {
		if h >= 32 {
			for i, l := range logo {
				top.WriteString(th.Gradient(strings.TrimRight(l, " "), m.frame/2+i, true) + "\n")
			}
			top.WriteString("\n")
		}
		top.WriteString(th.Gradient("C O M P A N Y   I N   A   B O X", m.frame/2, true))
	}
	tw := min(w, 76)
	var b strings.Builder
	b.WriteString(wrap("Your whole company on Microsoft 365, set up and secured in one window.", tw) + "\n\n")
	steps := []struct{ icon, text string }{
		{"①", "Answer five questions: what you run, how careful to be, who works here."},
		{"②", "Approve the plan, area by area. Nothing changes until you say so."},
		{"③", "Watch it build: mailboxes, sites, groups, policies. Coffee optional."},
	}
	if th.Plain {
		steps[0].icon, steps[1].icon, steps[2].icon = "1.", "2.", "3."
	}
	for _, s := range steps {
		b.WriteString(wrapHang(th.Accent.Render(s.icon)+" "+s.text, tw, "", "   ") + "\n")
	}
	b.WriteString("\n" + th.Muted.Render(wrap("About 15 minutes. Your answers are saved as you go, encrypted on this computer.", tw)))
	return center(top.String(), w) + "\n\n" + center(b.String(), w) + "\n\n" + center(m.welcomePaths(w), w)
}

// welcomePaths are the two ways in: set up, or a read-only audit (§26).
func (m *Model) welcomePaths(w int) string {
	th := m.th
	cw := min(60, w-4)
	paths := []struct{ label, detail string }{
		{"Set up and protect my company", "Build and secure Microsoft 365, step by step."},
		{"Prepare for an audit", "Read-only: check what you have against the frameworks."},
	}
	var rows []string
	for i, p := range paths {
		on := i == m.path
		switch {
		case th.Plain:
			mark := "  "
			if on {
				mark = "> "
			}
			rows = append(rows, mark+p.label+" - "+p.detail)
		case on:
			rows = append(rows, th.CardOn.Width(cw).Render(th.Gradient("▶  "+p.label, m.frame, true)+"\n"+th.Muted.Render("   "+p.detail)))
		default:
			rows = append(rows, th.Card.Width(cw).Render(th.Step.Render("   "+p.label)+"\n"+th.Muted.Render("   "+p.detail)))
		}
	}
	hint := "↑↓ choose · Press enter to begin"
	if !th.Plain {
		hint = th.Muted.Render("↑↓ choose · ") + th.Accent.Render("Press enter to begin")
	}
	return strings.Join(rows, "\n") + "\n" + hint
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
