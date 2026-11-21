package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Smohan-vcode/O365-Box/engine/internal/draft"
)

// reviewScreen is "Review and approve" (SPEC §25.1, §25.3): one card per
// area in plain words, with the number of settings behind it and the cost,
// and Approve / Show details / Not now. Decisions are stored in the draft as
// "approval.<area>" and passed to Options.Approve. The owner confirms by
// typing the company's domain (§18.4 step 7).
type reviewScreen struct {
	areas   []Area
	info    func(Selection) map[string]AreaInfo
	approve func(area, decision string) error
	domain  string

	cursor  int // an area, or len(areas) for the confirmation
	open    map[string]bool
	input   textinput.Model
	note    string
	noteErr bool
}

func newReviewScreen(opt Options) *reviewScreen {
	in := textinput.New()
	in.CharLimit = 253
	in.Width = 30
	in.Prompt = "> "
	return &reviewScreen{areas: opt.Areas, info: opt.AreaInfo, approve: opt.Approve, domain: opt.Domain,
		open: map[string]bool{}, input: in}
}

func (r *reviewScreen) ID() string    { return "review" }
func (r *reviewScreen) Title() string { return "Review" }
func (r *reviewScreen) Help() string {
	return "Each card is one part of your company: what we will set up there, in plain words, and how many settings are behind it.\n\nApprove what you are happy with. \"Not now\" leaves an area out; you can approve it later from the home screen.\n\nPolicies start in audit mode: they report what they would block, and nobody is blocked until you choose Enforce after the audit."
}
func (r *reviewScreen) Keys() []Key {
	if len(r.areas) == 0 {
		return nil
	}
	if r.cursor == len(r.areas) {
		return []Key{{"type", "your domain"}, {"enter", "confirm"}, {"↑", "back to the cards"}}
	}
	return []Key{{"↑↓", "move"}, {"a", "approve"}, {"d", "details"}, {"n", "not now"}, {"A", "approve all"}}
}

// confirmWord is what the owner types: the domain, or "approve" when the
// domain is not known yet.
func (r *reviewScreen) confirmWord(d *draft.Draft) string {
	if r.domain != "" {
		return r.domain
	}
	if v := d.String("company.domain"); v != "" {
		return v
	}
	return "approve"
}

func (r *reviewScreen) infos(d *draft.Draft) map[string]AreaInfo {
	if r.info == nil {
		return nil
	}
	return r.info(SelectionOf(d))
}

// open areas need a decision; an area that is off needs none.
func (r *reviewScreen) undecided(d *draft.Draft, infos map[string]AreaInfo) int {
	n := 0
	for _, a := range r.areas {
		if infos[a.ID].Off == "" && d.String(approvalKey(a.ID)) == "" {
			n++
		}
	}
	return n
}

func (r *reviewScreen) decide(d *draft.Draft, a Area, decision string) {
	if r.approve != nil {
		if err := r.approve(a.ID, decision); err != nil {
			r.note, r.noteErr = "Could not record that: "+err.Error(), true
			return
		}
	}
	d.Set(approvalKey(a.ID), decision)
	d.Set("review.confirmed", false)
	if decision == Approved {
		r.note, r.noteErr = "Approved: "+a.Title, false
	} else {
		r.note, r.noteErr = "Not now: "+a.Title+". You can approve it later from the home screen.", false
	}
}

func (r *reviewScreen) Update(msg tea.KeyMsg, d *draft.Draft) tea.Cmd {
	if len(r.areas) == 0 {
		return nil
	}
	r.note, r.noteErr = "", false
	infos := r.infos(d)
	if r.cursor == len(r.areas) {
		switch msg.String() {
		case "up":
			r.cursor--
			r.input.Blur()
			return nil
		case "enter":
			r.confirm(d, infos)
			return nil
		}
		var cmd tea.Cmd
		r.input, cmd = r.input.Update(msg)
		return cmd
	}
	a := r.areas[r.cursor]
	switch msg.String() {
	case "up", "k":
		if r.cursor > 0 {
			r.cursor--
		}
	case "down", "j":
		r.cursor++
		if r.cursor == len(r.areas) {
			r.input.Focus()
		}
	case "a", "enter", " ":
		if infos[a.ID].Off != "" {
			r.note = a.Title + " is off: " + infos[a.ID].Off
			break
		}
		r.decide(d, a, Approved)
		r.next(d, infos)
	case "n":
		if infos[a.ID].Off == "" {
			r.decide(d, a, NotNow)
			r.next(d, infos)
		}
	case "d":
		r.open[a.ID] = !r.open[a.ID]
	case "A":
		for _, x := range r.areas {
			if infos[x.ID].Off == "" && d.String(approvalKey(x.ID)) == "" {
				d.Set(approvalKey(x.ID), Approved)
				if r.approve != nil {
					if err := r.approve(x.ID, Approved); err != nil {
						r.note, r.noteErr = "Could not record that: "+err.Error(), true
						return nil
					}
				}
			}
		}
		d.Set("review.confirmed", false)
		r.note = "Every area approved. Type " + r.confirmWord(d) + " below to confirm."
		r.cursor = len(r.areas)
		r.input.Focus()
	}
	return nil
}

// next moves to the next area that needs a decision, or the confirmation.
func (r *reviewScreen) next(d *draft.Draft, infos map[string]AreaInfo) {
	for i := r.cursor + 1; i < len(r.areas); i++ {
		if infos[r.areas[i].ID].Off == "" && d.String(approvalKey(r.areas[i].ID)) == "" {
			r.cursor = i
			return
		}
	}
	if r.undecided(d, infos) == 0 {
		r.cursor = len(r.areas)
		r.input.Focus()
	}
}

func (r *reviewScreen) confirm(d *draft.Draft, infos map[string]AreaInfo) {
	if n := r.undecided(d, infos); n > 0 {
		r.note, r.noteErr = fmt.Sprintf("%d area(s) still need a decision: Approve or Not now.", n), true
		return
	}
	if !strings.EqualFold(strings.TrimSpace(r.input.Value()), r.confirmWord(d)) {
		r.note, r.noteErr = "Type "+r.confirmWord(d)+" exactly to confirm.", true
		return
	}
	d.Set("review.confirmed", true)
	r.input.Reset()
}

// chip is a short status label that does not rely on colour.
func chip(th Theme, decision, off string) string {
	switch {
	case off != "":
		if th.Plain {
			return "[off]"
		}
		return th.Muted.Render("– off")
	case decision == Approved:
		if th.Plain {
			return "[approved]"
		}
		return th.BadgeOK.Render("✓ approved")
	case decision == NotNow:
		if th.Plain {
			return "[not now]"
		}
		return th.Muted.Render("◌ not now")
	}
	if th.Plain {
		return "[to decide]"
	}
	return th.Badge.Render("? to decide")
}

func settingsNote(info AreaInfo, known bool) string {
	switch {
	case !known || info.Settings == 0:
		return ""
	case info.Settings == 1:
		return "1 setting"
	default:
		return fmt.Sprintf("%d settings", info.Settings)
	}
}

func (r *reviewScreen) View(width, height int, th Theme, d *draft.Draft) string {
	if len(r.areas) == 0 {
		return wrap("There is nothing to review here yet: the list of areas (defaults/areas.yaml) was not found.", width) + "\n\n" +
			th.Muted.Render(wrap("You can move on. Nothing changes in Microsoft 365 until you approve.", width))
	}
	infos := r.infos(d)
	approved, notNow := 0, 0
	for _, a := range r.areas {
		switch d.String(approvalKey(a.ID)) {
		case Approved:
			approved++
		case NotNow:
			notNow++
		}
	}
	left := r.undecided(d, infos)
	head := wrap("Here is what we will set up, area by area. Approve each one, or leave it for later.", width)
	tally := fmt.Sprintf("%d approved · %d to decide · %d not now", approved, left, notNow)
	var blocks []string // one per area, then the confirmation
	tw, nw := 0, 0
	for _, a := range r.areas {
		info, known := infos[a.ID]
		tw = max(tw, lipgloss.Width(a.Title))
		nw = max(nw, lipgloss.Width(settingsNote(info, known)))
	}
	for i, a := range r.areas {
		info, known := infos[a.ID]
		dec := d.String(approvalKey(a.ID))
		if i != r.cursor {
			// Title, count and status in columns.
			n := settingsNote(info, known)
			if info.Off != "" {
				n = ""
			}
			line := a.Title + strings.Repeat(" ", tw-lipgloss.Width(a.Title)) + "  " +
				th.Muted.Render(n+strings.Repeat(" ", nw-lipgloss.Width(n))) + "  " + chip(th, dec, info.Off)
			blocks = append(blocks, "    "+fit(line, width-4, 1))
			continue
		}
		blocks = append(blocks, r.card(th, width, a, info, known, dec))
	}
	blocks = append(blocks, r.confirmBlock(th, width, d, left))
	// Keep the cursor's block in view.
	room := height - strings.Count(head, "\n") - 4
	if r.note != "" {
		room -= 2
	}
	body := window(blocks, min(r.cursor, len(blocks)-1), room)
	out := head + "\n" + th.Muted.Render(tally) + "\n\n" + body
	if r.note != "" {
		st := th.OK
		if r.noteErr {
			st = th.Err
		}
		out += "\n\n" + st.Render(wrap(r.note, width))
	}
	return out
}

func (r *reviewScreen) card(th Theme, width int, a Area, info AreaInfo, known bool, dec string) string {
	inner := width - 6
	if th.Plain {
		inner = width - 4
	}
	var b strings.Builder
	title := th.Selected.Render(a.Title) + " " + chip(th, dec, info.Off)
	if n := settingsNote(info, known); n != "" && info.Off == "" {
		title += " " + th.Muted.Render("· "+n)
	}
	b.WriteString(title + "\n")
	if info.Off != "" {
		b.WriteString(th.Muted.Render(wrap("Off: "+info.Off, inner)))
	} else {
		b.WriteString(wrap(a.Plain, inner))
		for _, l := range info.Lines {
			b.WriteString("\n" + wrapHang("• "+l, inner, "", "  "))
		}
		cost := "Cost: none, included in your plan."
		if info.Cost != "" {
			cost = "Cost: " + info.Cost
		}
		b.WriteString("\n" + th.Muted.Render(wrap(cost, inner)))
		if !known {
			b.WriteString("\n" + th.Muted.Render(wrap("The settings behind this area are counted once your plan is ready.", inner)))
		}
		if r.open[a.ID] {
			b.WriteString("\n\n" + th.Accent.Render("Details") + "\n")
			if len(info.Details) == 0 {
				b.WriteString(th.Muted.Render(wrap("Built by: "+strings.Join(a.Steps, ", "), inner)))
			}
			var ds []string
			for i, l := range info.Details {
				if i == 8 {
					ds = append(ds, th.Muted.Render(fmt.Sprintf("  … and %d more", len(info.Details)-8)))
					break
				}
				ds = append(ds, fit("  "+l, inner, 1))
			}
			b.WriteString(strings.Join(ds, "\n"))
		}
		acts := "a Approve   d Show details   n Not now"
		if !th.Plain {
			acts = th.Key.Render("a") + " Approve   " + th.Key.Render("d") + " Show details   " + th.Key.Render("n") + " Not now"
		}
		b.WriteString("\n\n" + acts)
	}
	text := strings.TrimRight(b.String(), "\n")
	if th.Plain {
		return th.Cursor + " " + strings.ReplaceAll(text, "\n", "\n  ") + "\n"
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, th.Cursor+" ", th.CardOn.Width(inner+2).Render(text))
}

func (r *reviewScreen) confirmBlock(th Theme, width int, d *draft.Draft, left int) string {
	if ok, _ := d.Answers["review.confirmed"].(bool); ok {
		mark := "✓ "
		if th.Plain {
			mark = "[ok] "
		}
		return th.OK.Render(wrap(mark+"Confirmed. Press tab to build: policies start in audit mode, so nobody is blocked.", width))
	}
	word := r.confirmWord(d)
	ask := "To approve and build, type " + word + ":"
	if left > 0 {
		ask = fmt.Sprintf("Decide the %d remaining area(s), then type %s to confirm.", left, word)
	}
	mark := "  "
	if r.cursor == len(r.areas) {
		mark = th.Cursor + " "
	}
	return "\n" + mark + wrapHang(ask, width-2, "", "  ") + "\n  " + r.input.View()
}

// window joins blocks and cuts the result to room lines around block cur.
func window(blocks []string, cur, room int) string {
	var lines []string
	start, end := 0, 0
	for i, b := range blocks {
		if i == cur {
			start = len(lines)
		}
		lines = append(lines, strings.Split(b, "\n")...)
		if i == cur {
			end = len(lines)
		}
	}
	if room < 3 || len(lines) <= room {
		return strings.Join(lines, "\n")
	}
	first := max(0, min(start-(room-(end-start))/2, len(lines)-room))
	if end-first > room {
		first = start
	}
	last := min(len(lines), first+room)
	return strings.Join(lines[first:last], "\n")
}

// Ready keeps the owner here until every area is decided and confirmed.
func (r *reviewScreen) Ready(d *draft.Draft) error {
	if len(r.areas) == 0 {
		return nil
	}
	if ok, _ := d.Answers["review.confirmed"].(bool); !ok {
		return errors.New("approve or set aside each area, then type " + r.confirmWord(d) + " to confirm")
	}
	return nil
}
