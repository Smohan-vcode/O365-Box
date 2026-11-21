package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Smohan-vcode/O365-Box/engine/internal/draft"
)

// BuildEvent is one update from the build (the orchestrator's executor).
type BuildEvent struct {
	Step    string // step id
	Title   string // what people read
	State   string // queued | running | done | failed | waiting
	Changes int    // changes made by the step so far
	Detail  string // the latest thing it did, or why it waits
}

// BuildFeed starts the build and streams its events; the channel closes
// when the build ends.
type BuildFeed func() <-chan BuildEvent

type buildRow struct {
	BuildEvent
}

// buildScreen shows the build live (SPEC §18.4 step 8, T13-08).
type buildScreen struct {
	feed    BuildFeed
	events  <-chan BuildEvent
	rows    []*buildRow
	byID    map[string]*buildRow
	last    string
	started bool
	ended   bool
	frame   int
	outline []string
	home    bool // a home screen follows the build
}

func newBuildScreen(feed BuildFeed, outline []string) *buildScreen {
	return &buildScreen{feed: feed, byID: map[string]*buildRow{}, outline: outline}
}

func (b *buildScreen) ID() string    { return "build" }
func (b *buildScreen) Title() string { return "Build" }
func (b *buildScreen) Help() string {
	return "The build runs step by step. When something needs you, such as a security key, we show a short guide and wait.\n\nYou can leave this running: progress is saved, and box picks up where it stopped."
}
func (b *buildScreen) Keys() []Key {
	if b.feed != nil && !b.started {
		return []Key{{"enter", "start the build"}}
	}
	if b.ended && b.home {
		return []Key{{"enter", "home screen"}}
	}
	return nil
}
func (b *buildScreen) Ready(*draft.Draft) error { return nil }

// Animating keeps the spinner and confetti moving.
func (b *buildScreen) Animating() bool { return b.started && (!b.ended || b.frame < 400) }

func (b *buildScreen) Update(msg tea.KeyMsg, _ *draft.Draft) tea.Cmd {
	if msg.String() == "enter" && b.feed != nil && !b.started {
		b.events = b.feed()
		b.started = true
	}
	return nil
}

// Tick drains the events that arrived since the last frame.
func (b *buildScreen) Tick(*draft.Draft) {
	b.frame++
	if b.events == nil {
		return
	}
	for {
		select {
		case e, ok := <-b.events:
			if !ok {
				b.ended, b.events = true, nil
				return
			}
			b.apply(e)
		default:
			return
		}
	}
}

func (b *buildScreen) apply(e BuildEvent) {
	r := b.byID[e.Step]
	if r == nil {
		r = &buildRow{}
		b.byID[e.Step] = r
		b.rows = append(b.rows, r)
	}
	if e.Title == "" {
		e.Title = r.Title
	}
	r.BuildEvent = e
	if e.Detail != "" {
		b.last = e.Detail
	}
}

func (b *buildScreen) counts() (done, failed, total, changes int) {
	for _, r := range b.rows {
		switch r.State {
		case "done":
			done++
		case "failed":
			failed++
		}
		changes += r.Changes
	}
	return done, failed, len(b.rows), changes
}

func (b *buildScreen) View(width, height int, th Theme, _ *draft.Draft) string {
	var s strings.Builder
	if !b.started {
		s.WriteString(wrap("When you start, we build:", width) + "\n\n")
		for _, o := range b.outline {
			s.WriteString(wrapHang(th.Muted.Render(th.TodoMark)+" "+o, width, "", "  ") + "\n")
		}
		if b.feed != nil {
			s.WriteString("\n" + th.Accent.Render("Press enter to start the build."))
		} else {
			s.WriteString("\n" + th.Muted.Render(wrap("The live build is wired in task T13-08. Until then, run box apply.", width)))
		}
		return s.String()
	}
	done, failed, total, changes := b.counts()
	frac := 0.0
	if total > 0 {
		frac = float64(done) / float64(total)
	}
	if b.ended && failed == 0 {
		if c := confetti(th, b.frame/3, width, 2); c != "" {
			s.WriteString(c + "\n")
		}
		s.WriteString(th.Gradient("✦ Your company is ready! ✦", b.frame, true) + "\n")
	} else if b.ended {
		s.WriteString(th.Err.Render(fmt.Sprintf("%d step(s) need attention", failed)) + "\n")
	} else {
		s.WriteString(th.Accent.Render("Building your company…") + "\n")
	}
	s.WriteString(th.Bar(max(10, width-18), frac) + th.Muted.Render(fmt.Sprintf(" %d/%d steps", done, total)) + "\n\n")
	// Keep the running rows in view on small screens.
	rows := b.rows
	room := height - 8
	if room < 3 {
		room = 3
	}
	if len(rows) > room {
		first := 0
		for i, r := range rows {
			if r.State == "running" || r.State == "waiting" {
				first = i
				break
			}
			first = i
		}
		start := max(0, min(first-room/2, len(rows)-room))
		rows = rows[start : start+room]
	}
	for _, r := range rows {
		icon, st := th.TodoMark, th.Muted
		switch r.State {
		case "running":
			icon, st = spinner[b.frame%len(spinner)], th.Accent
			if th.Plain {
				icon = "[..]"
			}
		case "done":
			icon, st = "✓", th.OK
			if th.Plain {
				icon = "[ok]"
			}
		case "failed":
			icon, st = "✗", th.Err
			if th.Plain {
				icon = "[!!]"
			}
		case "waiting":
			icon, st = "⏸", th.Selected
			if th.Plain {
				icon = "[wait]"
			}
		}
		line := st.Render(icon) + " " + r.Title
		if r.Changes > 0 {
			line += th.Muted.Render(fmt.Sprintf("  +%d", r.Changes))
		}
		if r.State == "waiting" && r.Detail != "" {
			line += "  " + th.Selected.Render("needs you: "+r.Detail)
		}
		s.WriteString(fit(line, width, 1) + "\n")
	}
	if b.ended && failed == 0 {
		s.WriteString("\n" + th.OK.Render(wrap(fmt.Sprintf("%d change(s) made. A second run would change nothing.", changes), width)))
	}
	if b.ended && b.home {
		s.WriteString("\n" + th.Accent.Render("Press enter to open your home screen."))
	} else if b.last != "" && (!b.ended || failed > 0) {
		s.WriteString("\n" + th.Muted.Render(fit("› "+b.last, width, 1)))
	}
	return s.String()
}
