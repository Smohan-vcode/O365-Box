package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Smohan-vcode/O365-Box/engine/internal/choices"
	"github.com/Smohan-vcode/O365-Box/engine/internal/draft"
)

// choiceScreen is a single-choice list stored under one draft key.
type choiceScreen struct {
	id, title, question, help, key string
	options                        []option
	cursor                         int
}

type option struct{ Value, Label, Detail string }

func (c *choiceScreen) ID() string    { return c.id }
func (c *choiceScreen) Title() string { return c.title }
func (c *choiceScreen) Help() string  { return c.help }
func (c *choiceScreen) Keys() []Key {
	return []Key{{"↑↓", "move"}, {"enter/space", "choose"}}
}

func (c *choiceScreen) sync(d *draft.Draft) {
	v := d.String(c.key)
	for i, o := range c.options {
		if o.Value == v {
			c.cursor = i
		}
	}
}

func (c *choiceScreen) Update(msg tea.KeyMsg, d *draft.Draft) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		if c.cursor > 0 {
			c.cursor--
		}
	case "down", "j":
		if c.cursor < len(c.options)-1 {
			c.cursor++
		}
	case "enter", " ":
		d.Set(c.key, c.options[c.cursor].Value)
	}
	return nil
}

func (c *choiceScreen) View(width, _ int, th Theme, d *draft.Draft) string {
	var b strings.Builder
	b.WriteString(wrap(c.question, width) + "\n\n")
	chosen := d.String(c.key)
	for i, o := range c.options {
		b.WriteString(optionCard(th, width, i == c.cursor, o.Value == chosen, o.Label, o.Detail, nil) + "\n")
	}
	return b.String()
}

// optionCard renders one answer: a card in colour themes, a line in plain.
// badges are short labels such as "★ recommended" or "free".
func optionCard(th Theme, width int, cursor, chosen bool, label, detail string, badges []string) string {
	box := th.Unchecked
	if chosen {
		box = th.Checked
	}
	mark := "  "
	if cursor {
		mark = th.Cursor + " "
	}
	if th.Plain {
		line := fmt.Sprintf("%s%s %s", mark, box, label)
		for _, bd := range badges {
			line += " (" + bd + ")"
		}
		out := line
		if detail != "" {
			out += "\n" + wrapIndent(detail, width, "      ")
		}
		return out
	}
	head := box + " " + label
	if cursor || chosen {
		head = th.Selected.Render(head)
	}
	for i, bd := range badges {
		st := th.BadgeOK
		if i == 0 && strings.HasPrefix(bd, "★") {
			st = th.Badge
		}
		head += " " + st.Render(bd)
	}
	inner := width - 6
	body := head
	if detail != "" {
		body += "\n" + th.Muted.Render(wrap(detail, inner))
	}
	card := th.Card
	if cursor {
		card = th.CardOn
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, mark, card.Width(inner+2).Render(body))
}

func (c *choiceScreen) Ready(d *draft.Draft) error {
	if d.String(c.key) == "" {
		return fmt.Errorf("choose one option (space or enter)")
	}
	return nil
}

// plannedScreen describes a step whose screen a later task builds.
type plannedScreen struct {
	id, title, task, help string
	collects              []string
}

func (p *plannedScreen) ID() string                              { return p.id }
func (p *plannedScreen) Title() string                           { return p.title }
func (p *plannedScreen) Help() string                            { return p.help }
func (p *plannedScreen) Keys() []Key                             { return nil }
func (p *plannedScreen) Update(tea.KeyMsg, *draft.Draft) tea.Cmd { return nil }
func (p *plannedScreen) Ready(*draft.Draft) error                { return nil }
func (p *plannedScreen) View(width, _ int, th Theme, _ *draft.Draft) string {
	var b strings.Builder
	b.WriteString(wrap("This step will ask for:", width) + "\n\n")
	for _, c := range p.collects {
		b.WriteString(wrapHang("• "+c, width, "", "  ") + "\n")
	}
	b.WriteString("\n" + th.Muted.Render(wrap("This screen is built in task "+p.task+". You can move on; nothing is changed in Microsoft 365 until the Build step.", width)))
	return b.String()
}

// Steps returns the eight wizard steps (SPEC §18.4). With the choices
// catalogue, Security and Structure ask its questions (T13-10).
func Steps(cat *choices.Catalogue) []Screen {
	steps := []Screen{
		&choiceScreen{id: "start", title: "Start", key: "start.mode",
			question: "Do you already have Microsoft 365 for your company?",
			help:     "We sign you in to your Microsoft 365 organisation and read what Microsoft already knows: your organisation's name and address, domains and licences. Nothing is changed until the Build step.\n\nWhy we ask: if you do not have Microsoft 365 yet, we show you how to sign up first.",
			options: []option{
				{"existing", "Yes, sign me in", "You need the Global Administrator account you created when you signed up."},
				{"signup", "No, help me sign up", "We explain which plan to choose and open Microsoft's sign-up page."},
				{"other", "I'm setting this up for someone else", "You need that company's administrator sign-in."},
			}},
		&plannedScreen{id: "company", title: "Company", task: "T13-03, T13-05",
			help:     "Your company's legal details appear on emails, documents and notices. Values Microsoft already knows are filled in for you to confirm.",
			collects: []string{"Legal and trading name, entity wording", "Registered address, registration and tax numbers, telephone", "Domains", "Where the company is registered, and where it operates", "Email disclaimer, document footer and other legal notices"}},
		&plannedScreen{id: "addresses", title: "Addresses", task: "T13-03",
			help:     "Shared addresses such as info@ or support@ are mailboxes several people can read and reply from. They need no paid licence.",
			collects: []string{"Your own address", "Shared mailboxes, aliases and lists, and who reads each", "Automatic replies", "Who manages your DNS, and its access token"}},
		&plannedScreen{id: "security", title: "Security", task: "T13-04",
			help:     "Security settings protect sign-in, email, sharing, meetings and devices. The recommended level suits most companies.",
			collects: []string{"Security level: essential, recommended or strict", "Any setting you want to change, with a reason", "Emergency (break-glass) administrator accounts"}},
		&plannedScreen{id: "structure", title: "Structure", task: "T13-06",
			help:     "Your files live in sites and libraries. You can add folders and decide which groups can open what.",
			collects: []string{"Modules to switch on", "Sites, libraries and folders", "Groups, and who can open what", "Your legal pack, if you have one"}},
		&plannedScreen{id: "people", title: "People", task: "T13-03",
			help:     "Each person gets an account, a plan and the groups for their role. We show how many licences you have left.",
			collects: []string{"Name, address, plan and roles for each person", "How each person gets their first sign-in pass: on screen or by email to a private address"}},
		&plannedScreen{id: "review", title: "Review", task: "T13-07",
			help:     "Check everything that will be built before anything changes.",
			collects: []string{"The full plan, with every change", "Licence use, manual steps ahead and estimated time", "Your typed confirmation"}},
		newBuildScreen(nil, []string{"Your people, groups and emergency accounts", "Shared mailboxes, lists and automatic replies",
			"Sites, libraries and the company registers", "Security policies, audit mode first", "A final report"}),
	}
	if cat != nil {
		for i, st := range steps {
			p, ok := st.(*plannedScreen)
			if !ok || (p.id != "security" && p.id != "structure") {
				continue
			}
			cs := newChoicesScreen(p.id, p.title, p.help, cat, p.collects)
			if len(cs.questions) > 0 {
				steps[i] = cs
			}
		}
	}
	return steps
}
