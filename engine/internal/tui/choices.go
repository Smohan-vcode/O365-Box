package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Smohan-vcode/O365-Box/engine/internal/choices"
	"github.com/Smohan-vcode/O365-Box/engine/internal/draft"
)

// choicesScreen asks the choices of one wizard step (SPEC §24, T13-10),
// one question at a time, with the recommended option preselected.
// Answers are stored in the draft as "choices.<id>".
type choicesScreen struct {
	id, title, help string
	questions       []*choices.Choice
	also            []string // what the step's other screens (later tasks) collect
	q, cursor       int
	ctx             choices.Context // the company's modules, for defaults
	level           *choices.Level  // the careful dial's level; nil uses each choice's default
}

func (s *choicesScreen) ID() string    { return s.id }
func (s *choicesScreen) Title() string { return s.title }
func (s *choicesScreen) Help() string {
	if len(s.questions) == 0 {
		return s.help
	}
	ch := s.questions[s.q]
	h := ch.Help
	if h == "" {
		h = s.help
	}
	if ch.When != "" {
		h += "\n\nAsked when the " + strings.TrimPrefix(ch.When, "module ") + " module is on."
	}
	return h + "\n\nEvery answer can be changed later from the home screen."
}
func (s *choicesScreen) Keys() []Key {
	return []Key{{"↑↓", "move"}, {"enter", "choose"}, {"←→", "question"}}
}

func key4(id string) string { return "choices." + id }

// answer is the stored answer or the recommended default.
func (s *choicesScreen) answer(d *draft.Draft, ch *choices.Choice) (string, bool) {
	if v := d.String(key4(ch.ID)); v != "" {
		return v, true
	}
	return ch.DefaultAt(s.level, s.ctx), false
}

func (s *choicesScreen) sync(d *draft.Draft) {
	if len(s.questions) == 0 {
		return
	}
	v, _ := s.answer(d, s.questions[s.q])
	for i, o := range s.questions[s.q].Options {
		if o.Value == v {
			s.cursor = i
		}
	}
}

func (s *choicesScreen) Update(msg tea.KeyMsg, d *draft.Draft) tea.Cmd {
	if len(s.questions) == 0 {
		return nil
	}
	ch := s.questions[s.q]
	switch msg.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(ch.Options)-1 {
			s.cursor++
		}
	case "left", "h", "[":
		if s.q > 0 {
			s.q--
			s.sync(d)
		}
	case "right", "l", "]":
		if s.q < len(s.questions)-1 {
			s.q++
			s.sync(d)
		}
	case "enter", " ":
		d.Set(key4(ch.ID), ch.Options[s.cursor].Value)
		if s.q < len(s.questions)-1 {
			s.q++
			s.sync(d)
		}
	}
	return nil
}

func (s *choicesScreen) View(width, _ int, th Theme, d *draft.Draft) string {
	var b strings.Builder
	if len(s.questions) == 0 {
		return wrap("Nothing to decide here.", width)
	}
	// Question dots: answered, current, still on the recommendation.
	var dots []string
	for i, ch := range s.questions {
		_, set := s.answer(d, ch)
		mark := th.TodoMark
		switch {
		case i == s.q:
			mark = th.Accent.Render(th.CurMark)
		case set:
			mark = th.OK.Render(th.DoneMark)
		}
		dots = append(dots, mark)
	}
	label := th.Muted.Render(fmt.Sprintf("   question %d of %d", s.q+1, len(s.questions)))
	progress := strings.Join(dots, " ")
	if lipgloss.Width(progress)+lipgloss.Width(label) > width {
		// Many questions: a bar instead of one dot each.
		progress = th.Bar(max(8, min(30, width-lipgloss.Width(label)-2)), float64(s.q)/float64(max(1, len(s.questions)-1)))
	}
	b.WriteString(progress + label + "\n\n")
	ch := s.questions[s.q]
	b.WriteString(th.Accent.Render(wrap(ch.Question, width)) + "\n\n")
	chosen, _ := s.answer(d, ch)
	def := ch.Default(s.ctx)
	for i, o := range ch.Options {
		var badges []string
		if o.Value == def {
			badges = append(badges, "★ recommended")
		}
		if o.Value == ch.Strict {
			badges = append(badges, "stricter")
		}
		if o.Cost == "none" {
			badges = append(badges, "free")
		}
		detail := o.Detail
		if e := effectNote(o, o.Cost); e != "" {
			if detail != "" {
				detail += " "
			}
			detail += e
		}
		b.WriteString(optionCard(th, width, i == s.cursor, o.Value == chosen, o.Label, detail, badges) + "\n")
	}
	if len(s.also) > 0 && s.q == len(s.questions)-1 {
		b.WriteString("\n" + th.Muted.Render(wrap("Also on this step later: "+strings.Join(s.also, "; ")+".", width)))
	}
	return b.String()
}

// effectNote says in plain words what an option changes beyond its label.
func effectNote(o choices.Option, cost string) string {
	var parts []string
	if cost != "none" && cost != "" {
		parts = append(parts, "Cost: "+cost+".")
	}
	if n := len(o.Effects.Exceptions); n > 0 {
		parts = append(parts, fmt.Sprintf("Leaves out %d setting(s).", n))
	}
	if n := len(o.Effects.Gates); n > 0 {
		parts = append(parts, fmt.Sprintf("Adds %d short manual step(s).", n))
	}
	if len(o.Effects.Bundles) > 0 {
		parts = append(parts, "Setup app needs: "+strings.Join(o.Effects.Bundles, ", ")+" permissions.")
	}
	return strings.Join(parts, " ")
}

// Ready never blocks: unanswered choices keep their recommendation.
func (s *choicesScreen) Ready(*draft.Draft) error { return nil }
