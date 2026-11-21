package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Smohan-vcode/O365-Box/engine/internal/choices"
	"github.com/Smohan-vcode/O365-Box/engine/internal/draft"
)

// carefulScreen is "How careful?" (SPEC §25.3): one dial, Essential,
// Recommended or Strict, that sets policy.tier and the answer of every
// choice from defaults/choices.yaml, and a "fine-tune" card that opens the
// individual choices. Draft keys: careful, careful.tuned, policy.tier and
// choices.<id>.
type carefulScreen struct {
	cat    *choices.Catalogue
	help   string
	cursor int            // a level, or len(levels) for fine-tune
	tuning *choicesScreen // the individual choices while fine-tuning
}

func newCarefulScreen(cat *choices.Catalogue, help string) *carefulScreen {
	return &carefulScreen{cat: cat, help: help}
}

func (c *carefulScreen) levels() []choices.Level { return c.cat.Dial.Levels }

func (c *carefulScreen) ID() string    { return "careful" }
func (c *carefulScreen) Title() string { return "How careful" }
func (c *carefulScreen) Help() string {
	if c.tuning != nil {
		return c.tuning.Help()
	}
	if c.cat.Dial.Help != "" {
		return c.cat.Dial.Help
	}
	return c.help
}
func (c *carefulScreen) Keys() []Key {
	if c.tuning != nil {
		return append(c.tuning.Keys(), Key{"esc", "back to the dial"})
	}
	return []Key{{"↑↓←→", "turn the dial"}, {"enter", "choose"}}
}

// ctx is the company context from the draft: the modules switched on.
func ctxOf(d *draft.Draft) choices.Context {
	return choices.Context{Modules: SelectionOf(d).Modules}
}

func (c *carefulScreen) sync(d *draft.Draft) {
	c.cursor = c.recIndex()
	for i, l := range c.levels() {
		if l.Value == d.String("careful") {
			c.cursor = i
		}
	}
	if b, _ := d.Answers["careful.tuned"].(bool); b && c.tuning == nil {
		c.cursor = len(c.levels())
	}
}

func (c *carefulScreen) recIndex() int {
	for i, l := range c.levels() {
		if l.Recommended {
			return i
		}
	}
	return 0
}

// apply sets the dial: the tier and an explicit answer for every choice
// that applies, so the Review lists nothing filled silently.
func (c *carefulScreen) apply(d *draft.Draft, level string) {
	ctx := ctxOf(d)
	tier, answers, err := c.cat.DialAnswers(level, ctx)
	if err != nil {
		return
	}
	d.Set("careful", level)
	d.Set("careful.tuned", false)
	d.Set("policy.tier", tier)
	for i := range c.cat.Choices {
		id := c.cat.Choices[i].ID
		if runOwned(id) {
			continue
		}
		if v, ok := answers[id]; ok {
			d.Set(key4(id), v)
		} else {
			delete(d.Answers, key4(id)) // the choice does not apply to this company
		}
	}
}

// fineTune opens the individual choices, starting from the dial's answers.
func (c *carefulScreen) fineTune(d *draft.Draft) {
	if d.String("careful") == "" {
		c.apply(d, c.levels()[c.recIndex()].Value)
	}
	d.Set("careful.tuned", true)
	ctx := ctxOf(d)
	s := &choicesScreen{id: "careful", title: "How careful", help: c.help, ctx: ctx, level: c.cat.Dial.Level(d.String("careful"))}
	for i := range c.cat.Choices {
		ch := &c.cat.Choices[i]
		if !runOwned(ch.ID) && ch.Applies(ctx) {
			s.questions = append(s.questions, ch)
		}
	}
	s.sync(d)
	c.tuning = s
}

func (c *carefulScreen) Update(msg tea.KeyMsg, d *draft.Draft) tea.Cmd {
	if c.tuning != nil {
		if msg.String() == "esc" {
			c.tuning = nil
			return nil
		}
		return c.tuning.Update(msg, d)
	}
	n := len(c.levels())
	switch msg.String() {
	case "up", "k", "left", "h":
		if c.cursor > 0 {
			c.cursor--
		}
	case "down", "j", "right", "l":
		if c.cursor < n {
			c.cursor++
		}
	case "enter", " ":
		if c.cursor < n {
			c.apply(d, c.levels()[c.cursor].Value)
		} else {
			c.fineTune(d)
		}
	}
	return nil
}

func (c *carefulScreen) View(width, height int, th Theme, d *draft.Draft) string {
	if c.tuning != nil {
		head := th.Accent.Render("Fine-tune") + th.Muted.Render(" · starting from "+c.levelLabel(d.String("careful"))+" · esc: dial")
		return fit(head, width, 1) + "\n\n" + c.tuning.View(width, height-2, th, d)
	}
	var b strings.Builder
	b.WriteString(wrap(c.cat.Dial.Question, width) + "\n\n")
	b.WriteString(c.dial(th, width) + "\n\n")
	chosen := d.String("careful")
	if chosen == "" {
		chosen = c.levels()[c.recIndex()].Value // preselected: moving on keeps it
	}
	tuned, _ := d.Answers["careful.tuned"].(bool)
	ctx := ctxOf(d)
	for i, l := range c.levels() {
		var badges []string
		if l.Recommended {
			badges = append(badges, "★ our suggestion")
		}
		detail := ""
		if i == c.cursor {
			detail = l.Detail
		}
		b.WriteString(optionCard(th, width, i == c.cursor, l.Value == chosen && !tuned, l.Label, detail, badges) + "\n")
	}
	n := 0
	for i := range c.cat.Choices {
		if !runOwned(c.cat.Choices[i].ID) && c.cat.Choices[i].Applies(ctx) {
			n++
		}
	}
	ft := ""
	if c.cursor == len(c.levels()) {
		ft = fmt.Sprintf("Go through the %d individual choices yourself, starting from the dial's answers.", n)
	}
	b.WriteString(optionCard(th, width, c.cursor == len(c.levels()), tuned, "Let me fine-tune", ft, nil) + "\n")
	if c.cursor < len(c.levels()) {
		b.WriteString("\n" + c.differences(th, width, &c.levels()[c.cursor], ctx))
	}
	return b.String()
}

func (c *carefulScreen) levelLabel(v string) string {
	if l := c.cat.Dial.Level(v); l != nil {
		return l.Label
	}
	return "the recommendation"
}

// differences lists what a level answers differently from the
// recommendation, in the words of the choices.
func (c *carefulScreen) differences(th Theme, width int, l *choices.Level, ctx choices.Context) string {
	var lines []string
	for i := range c.cat.Choices {
		ch := &c.cat.Choices[i]
		if runOwned(ch.ID) || !ch.Applies(ctx) {
			continue
		}
		if v := ch.DefaultAt(l, ctx); v != ch.Default(ctx) {
			lines = append(lines, wrapHang("• "+ch.Question+": "+ch.Option(v).Label, width, "", "  "))
		}
	}
	tierNote := ""
	if l.Tier == "enhanced" {
		tierNote = " Also switches on the stricter settings (the enhanced tier)."
	}
	if len(lines) == 0 {
		return th.Muted.Render(wrap("Every choice stays on our recommendation."+tierNote, width))
	}
	return th.Muted.Render(wrap("Compared with our recommendation:"+tierNote, width)) + "\n" + strings.Join(lines, "\n")
}

// dial draws the levels as stops on a track, the cursor's stop lit.
func (c *carefulScreen) dial(th Theme, width int) string {
	ls := c.levels()
	n := len(ls)
	if n < 2 {
		return ""
	}
	tw := min(width-4, 50)
	pos := make([]int, n)
	for i := range ls {
		pos[i] = i * (tw - 1) / (n - 1)
	}
	if th.Plain {
		var parts []string
		for i, l := range ls {
			if i == c.cursor {
				parts = append(parts, "[*"+l.Label+"*]")
			} else {
				parts = append(parts, l.Label)
			}
		}
		return "Dial: " + strings.Join(parts, " --- ")
	}
	var track strings.Builder
	stop := 0
	for x := 0; x < tw; x++ {
		if stop < n && x == pos[stop] {
			if stop == c.cursor {
				track.WriteString(th.Accent.Render("◉"))
			} else {
				track.WriteString(th.Muted.Render("○"))
			}
			stop++
			continue
		}
		track.WriteString(lipgloss.NewStyle().Foreground(th.Ramp[x*len(th.Ramp)/tw]).Render("━"))
	}
	// Labels centred under their stops, kept inside the track.
	labels := []rune(strings.Repeat(" ", tw+2))
	var marks []struct{ from, to int }
	for i, l := range ls {
		w := len([]rune(l.Label))
		from := max(0, min(pos[i]-w/2, tw-w))
		copy(labels[from:], []rune(l.Label))
		marks = append(marks, struct{ from, to int }{from, from + w})
	}
	var lab strings.Builder
	for i, m := range marks {
		prev := 0
		if i > 0 {
			prev = marks[i-1].to
		}
		lab.WriteString(string(labels[prev:m.from]))
		text := string(labels[m.from:m.to])
		if i == c.cursor {
			lab.WriteString(th.Accent.Render(text))
		} else {
			lab.WriteString(th.Muted.Render(text))
		}
	}
	return "  " + track.String() + "\n  " + lab.String()
}

// Ready sets the recommended level when the owner moves on without
// turning the dial: the preselected answer, written explicitly.
func (c *carefulScreen) Ready(d *draft.Draft) error {
	c.tuning = nil
	if d.String("careful") == "" {
		c.apply(d, c.levels()[c.recIndex()].Value)
	}
	return nil
}
