package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The "Prepare for an audit" path (SPEC §26): this screen explains the
// read-only audit and hands over to Options.Audit (box audit). It writes
// nothing: not the tenant, not the draft.

func (m *Model) updateAudit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status, m.isErr = "", false
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "shift+tab", "backspace":
		m.phase = phaseWelcome
	case "enter":
		if m.opt.Audit == nil {
			m.status = "The audit is not in this version of box yet. When it is, start it with: box audit"
			return m, nil
		}
		m.AuditChosen = true
		return m, tea.Quit
	case "ctrl+t":
		m.themeIdx = (m.themeIdx + 1) % len(themeCycle)
		m.th = ThemeFor(themeCycle[m.themeIdx])
	}
	return m, nil
}

func (m *Model) viewAudit(w int) string {
	th := m.th
	tw := min(w, 84)
	var b strings.Builder
	if th.Plain {
		b.WriteString("Prepare for an audit\n\n")
	} else {
		b.WriteString(th.Gradient("◎ Prepare for an audit", m.frame/2, true) + "\n\n")
	}
	ro := "Read-only."
	if !th.Plain {
		ro = th.BadgeOK.Render("read-only") + " " + th.Badge.Render("no changes")
	}
	b.WriteString(ro + "\n\n")
	b.WriteString(wrap("We look at your Microsoft 365 as it is today and write an audit pack: what meets each framework's controls, what partly meets them, and the gaps in plain words, ordered by risk. Nothing in your Microsoft 365 is changed.", tw) + "\n\n")
	b.WriteString(th.Accent.Render("What we read") + "\n")
	for _, l := range []string{
		"Every security setting we know about, and its live value",
		"People and administrators, two-step sign-in and last sign-in; roles and guests",
		"Sign-in rules, devices and their compliance, sharing and mail forwarding",
		"Labels, leak prevention, retention, the audit log and alerts; licences",
		"Activity over the audit period, where Microsoft keeps it",
	} {
		b.WriteString(wrapHang("• "+l, tw, "", "  ") + "\n")
	}
	b.WriteString("\n" + th.Accent.Render("What you need") + "\n")
	b.WriteString(wrapHang("• A person with the Global Reader role (and Security Reader and Compliance Reader). No administrator changes, no app is installed.", tw, "", "  ") + "\n")
	b.WriteString(wrapHang("• A folder on this computer for the pack: a report you can open in a browser, a spreadsheet of controls, the evidence and the list of gaps.", tw, "", "  ") + "\n\n")
	if m.status != "" {
		b.WriteString(th.Selected.Render(wrap(m.status, tw)) + "\n\n")
	}
	keys := "enter start the audit · esc back · ctrl+t theme · q quit"
	if !th.Plain {
		keys = th.Key.Render("enter") + " start the audit · " + th.Key.Render("esc") + " back · " + th.Key.Render("ctrl+t") + " theme · " + th.Key.Render("q") + " quit"
	}
	b.WriteString(keys)
	return b.String()
}
