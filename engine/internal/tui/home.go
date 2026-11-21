package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The home screen (SPEC §18.6, §25.3) after the build: one status per
// area from the last check (protected, watching in audit, needs you) and
// Enforce for an area whose audit period is clean. Options.Home feeds it;
// Options.Enforce asks for the enforce proposal.

func (m *Model) openHome() {
	m.phase = phaseHome
	m.homeCur = 0
	m.home = m.opt.Home()
	m.loadRequests()
	m.status, m.isErr = "", false
}

func (m *Model) areaTitle(id string) string {
	for _, a := range m.opt.Areas {
		if a.ID == id {
			return a.Title
		}
	}
	return id
}

func (m *Model) updateHome(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.rejecting != 0 {
		return m.updateReason(msg)
	}
	m.status, m.isErr = "", false
	if m.updateRequestKey(msg) {
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		return m, tea.Quit
	case "up", "k", "left", "h":
		if m.homeCur > 0 {
			m.homeCur--
		}
	case "down", "j", "right", "l":
		if m.homeCur < m.homeRows()-1 {
			m.homeCur++
		}
	case "r":
		m.home = m.opt.Home()
		m.loadRequests()
		m.homeCur = min(m.homeCur, max(0, m.homeRows()-1))
		m.status = "Status refreshed from the last check."
	case "e":
		if m.homeCur >= len(m.home) {
			break
		}
		a := m.home[m.homeCur]
		title := m.areaTitle(a.Area)
		switch {
		case !a.CanEnforce:
			m.status = "Enforce is offered once " + title + " has a clean audit period."
		case m.opt.Enforce == nil:
			m.status = "Enforce is not available in this version yet."
		default:
			if err := m.opt.Enforce(a.Area); err != nil {
				m.setErr(err)
			} else {
				m.status = "Enforce proposed for " + title + ": approve it to switch audit mode off."
			}
		}
	case "ctrl+t":
		m.themeIdx = (m.themeIdx + 1) % len(themeCycle)
		m.th = ThemeFor(themeCycle[m.themeIdx])
	}
	return m, nil
}

// statusChip is the area's status in words and a symbol, never colour only.
func statusChip(th Theme, status string) string {
	type c struct {
		sym, plain, text string
		st               lipgloss.Style
	}
	var x c
	switch status {
	case "protected":
		x = c{"✓", "[protected]", "protected", th.BadgeOK}
	case "watching":
		x = c{"◐", "[watching]", "watching (audit)", th.Badge}
	default:
		x = c{"!", "[needs you]", "needs you", th.Badge.Background(th.Err.GetForeground())}
	}
	if th.Plain {
		return x.plain + strings.Repeat(" ", 11-len(x.plain))
	}
	const w = len("watching (audit)")
	return x.st.Render(x.sym + " " + x.text + strings.Repeat(" ", w-len(x.text)))
}

func (m *Model) viewHome(w, h int) string {
	th := m.th
	sep := strings.Repeat("─", w)
	if th.Plain {
		sep = strings.Repeat("-", w)
	}
	counts := map[string]int{}
	for _, a := range m.home {
		counts[a.Status]++
	}
	var head string
	if th.Plain {
		head = "Company in a Box - Home"
	} else {
		head = th.Gradient("▣ Company in a Box · Home", m.frame/2, true)
	}
	head += "\n" + th.Muted.Render(fmt.Sprintf("%d protected · %d watching (audit) · %d need you", counts["protected"], counts["watching"], counts["needs-you"]))
	var b strings.Builder
	b.WriteString(th.Title.Render("Your company at a glance") + "\n\n")
	if len(m.home) == 0 {
		b.WriteString(wrap("No check has run yet. Run box status, then come back here.", w) + "\n")
	}
	tw := 0
	for _, a := range m.home {
		tw = max(tw, lipgloss.Width(m.areaTitle(a.Area)))
	}
	for i, a := range m.home {
		mark := "  "
		if i == m.homeCur {
			mark = th.Cursor + " "
		}
		t := m.areaTitle(a.Area)
		row := mark + t + strings.Repeat(" ", tw-lipgloss.Width(t)) + "  " + statusChip(th, a.Status)
		if a.CanEnforce {
			if th.Plain {
				row += "  (e: Enforce)"
			} else {
				row += "  " + th.Key.Render("e") + " Enforce"
			}
		}
		if i == m.homeCur {
			row = th.Selected.Render(mark+t) + strings.TrimPrefix(row, mark+t)
		}
		b.WriteString(fit(row, w, 1) + "\n")
		if a.Note != "" && i == m.homeCur {
			b.WriteString(th.Muted.Render(fit("    "+a.Note, w, 1)) + "\n")
		}
	}
	b.WriteString(m.viewRequests(w))
	b.WriteString("\n" + th.Muted.Render(wrap("Policies start by watching (audit): they report what they would block and block nobody. Enforce an area once its audit period is clean.", w)))
	status := ""
	if m.status != "" {
		st := th.OK
		if m.isErr {
			st = th.Err
		}
		status = st.Render(m.status)
	}
	keys := []Key{{"↑↓", "move"}, {"e", "enforce"}, {"r", "refresh"}, {"ctrl+t", "theme"}, {"q", "quit"}}
	if m.curRequest() != nil {
		keys = []Key{{"↑↓", "move"}, {"a", "approve"}, {"i", "approve, install for everyone"}, {"x", "turn down"}, {"q", "quit"}}
	}
	if m.rejecting != 0 {
		keys = []Key{{"enter", "turn down"}, {"esc", "cancel"}}
	}
	var parts []string
	for _, k := range keys {
		parts = append(parts, th.Key.Render(k.Keys)+" "+k.Does)
	}
	bodyH := max(3, h-2-1-1-1-1)
	return strings.Join([]string{fit(head, w, 2), sep, fit(b.String(), w, bodyH), sep, fit(status, w, 1), fit(strings.Join(parts, " · "), w, 1)}, "\n")
}
