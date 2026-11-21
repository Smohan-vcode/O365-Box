package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Smohan-vcode/O365-Box/engine/internal/draft"
)

// runSwitch is one card of "What do you run?" (SPEC §25.3). It switches a
// module (§18.4) or answers a structure choice (§24).
type runSwitch struct {
	label, detail string
	module        string // config module, stored as "modules.<name>"
	choice        string // or a choice id, stored as "choices.<id>"
	on, off       string // the choice's option for on and off
	def           bool   // on by default
}

// runSwitches are the cards, in order. Module defaults follow SPEC §18.4.
var runSwitches = []runSwitch{
	{label: "Computers and phones", detail: "Company laptops and phones are encrypted, kept up to date and can be wiped if lost.", module: "devices", def: true},
	{label: "Files and records", detail: "Company sites and libraries, with registers for contracts, policies and other records.", module: "records", def: true},
	{label: "HR and people", detail: "Joiners, movers and leavers: accounts and access follow each person's role.", module: "people", def: true},
	{label: "Sales", detail: "A shared sales inbox, a customer list and public forms such as demo requests.", module: "sales"},
	{label: "Legal documents", detail: "Your contract and policy templates, kept in one place with signed copies filed.", module: "legal"},
	{label: "Microsoft Azure", detail: "Azure is Microsoft's cloud for servers and apps. We apply its free safety settings; nothing is billed until you create resources.", choice: "azure.subscription", on: "existing", off: "none"},
	{label: "Our own servers (Active Directory)", detail: "Accounts are also kept on your own servers. We copy them to Microsoft 365 with Cloud Sync.", choice: "directory.mode", on: "hybrid", off: "cloud"},
}

func (sw runSwitch) key() string {
	if sw.module != "" {
		return "modules." + sw.module
	}
	return key4(sw.choice)
}

// switchOn reads a switch from the draft, or its default.
func switchOn(d *draft.Draft, sw runSwitch) bool {
	if sw.module != "" {
		if v, ok := d.Answers[sw.key()].(bool); ok {
			return v
		}
		return sw.def
	}
	if v := d.String(sw.key()); v != "" {
		return v != sw.off
	}
	return sw.def
}

// runOwned are the choices "What do you run?" answers, so the careful dial
// leaves them alone.
func runOwned(id string) bool {
	for _, sw := range runSwitches {
		if sw.choice == id {
			return true
		}
	}
	return false
}

type runScreen struct {
	cursor   int
	detected map[string]bool // discovery facts (choices detect keys)
}

func (r *runScreen) ID() string    { return "run" }
func (r *runScreen) Title() string { return "What you run" }
func (r *runScreen) Help() string {
	return "Switch on what your company uses. Each switch adds the matching sites, groups and protection, and leaves out what you do not need.\n\nWhy we ask: a part you switch off produces no steps and no settings. You can switch parts on later from the home screen."
}
func (r *runScreen) Keys() []Key { return []Key{{"↑↓", "move"}, {"space", "switch on/off"}} }

func (r *runScreen) Update(msg tea.KeyMsg, d *draft.Draft) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		if r.cursor > 0 {
			r.cursor--
		}
	case "down", "j":
		if r.cursor < len(runSwitches)-1 {
			r.cursor++
		}
	case "enter", " ":
		r.set(d, runSwitches[r.cursor], !switchOn(d, runSwitches[r.cursor]))
	}
	return nil
}

func (r *runScreen) set(d *draft.Draft, sw runSwitch, on bool) {
	if sw.module != "" {
		d.Set(sw.key(), on)
		return
	}
	v := sw.off
	if on {
		v = sw.on
		// No subscription found: help create one (SPEC §24, azure.subscription).
		if sw.choice == "azure.subscription" && !r.detected["azure.subscriptions"] {
			v = "create"
		}
	}
	d.Set(sw.key(), v)
}

func (r *runScreen) View(width, height int, th Theme, d *draft.Draft) string {
	var b strings.Builder
	b.WriteString(wrap("What does your company run? Switch on what you use.", width) + "\n\n")
	n := 0
	for _, sw := range runSwitches {
		if switchOn(d, sw) {
			n++
		}
	}
	for i, sw := range runSwitches {
		on := switchOn(d, sw)
		var badges []string
		if on {
			badges = append(badges, "on")
		}
		detail := sw.detail
		if i != r.cursor {
			detail = "" // keep the list short: the card under the cursor explains itself
		}
		b.WriteString(toggleCard(th, width, i == r.cursor, on, sw.label, detail, badges) + "\n")
	}
	b.WriteString("\n" + th.Muted.Render(wrap(fmt.Sprintf("%d of %d switched on. Sign-in, email, files and meetings are always protected.", n, len(runSwitches)), width)))
	return b.String()
}

// toggleCard is an optionCard with a switch instead of a radio mark.
func toggleCard(th Theme, width int, cursor, on bool, label, detail string, badges []string) string {
	t := th
	t.Checked, t.Unchecked = "●━", "━○"
	if th.Plain {
		t.Checked, t.Unchecked = "[on] ", "[off]"
		badges = nil
	}
	if !cursor && !th.Plain {
		// A one-line row, aligned with the text inside the cursor's card.
		box := t.Muted.Render(t.Unchecked)
		head := label
		if on {
			box, head = t.OK.Render(t.Checked), t.Step.Render(label)
		} else {
			head = t.Muted.Render(label)
		}
		return "    " + box + " " + head
	}
	return optionCard(t, width, cursor, on, label, detail, badges)
}

// Ready writes every switch explicitly, so nothing is filled silently.
func (r *runScreen) Ready(d *draft.Draft) error {
	for _, sw := range runSwitches {
		if _, ok := d.Answers[sw.key()]; !ok {
			r.set(d, sw, sw.def)
		}
	}
	return nil
}
