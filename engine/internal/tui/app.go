package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Smohan-vcode/O365-Box/engine/internal/choices"
	"github.com/Smohan-vcode/O365-Box/engine/internal/draft"
)

// Options configure the TUI.
type Options struct {
	DraftPath string             // encrypted draft file
	Theme     string             // auto | dark | light | plain
	Keys      draft.KeySource    // fixed key source (tests); nil picks keychain or passphrase
	Keychain  func() bool        // reports keychain availability; default draft.KeychainAvailable
	Choices   *choices.Catalogue // defaults/choices.yaml; nil keeps "How careful" as an outline
	Build     BuildFeed          // starts the build; nil shows the outline only

	// Review and approve (SPEC §25).
	Areas    []Area                              // defaults/areas.yaml; nil shows an empty Review
	AreaInfo func(Selection) map[string]AreaInfo // the plan by area; nil leaves the counts out
	Approve  func(area, decision string) error   // records a decision (Approved or NotNow)
	Domain   string                              // the primary domain typed to confirm; "" uses the draft's
	Detected map[string]bool                     // discovery facts by choices detect key

	// After the build.
	Home    func() []AreaStatus // status per area from the last check; nil: no home screen
	Enforce func(area string) error

	// Audit runs the read-only audit (box audit, SPEC §26) after the TUI
	// closes; nil explains the path only.
	Audit func()
}

type phase int

const (
	phaseUnlock phase = iota
	phaseWelcome
	phaseWizard
	phaseAudit
	phaseHome
)

// The two ways in from the welcome screen.
const (
	pathSetup = iota
	pathAudit
)

// Model is the Bubble Tea model of the onboarding wizard.
type Model struct {
	opt      Options
	steps    []Screen
	cur      int
	d        *draft.Draft
	store    *draft.Store
	th       Theme
	themeIdx int
	w, h     int
	help     bool
	helpSet  bool
	status   string
	isErr    bool
	phase    phase
	pass     textinput.Model
	newDraft bool
	frame    int  // animation frame (tickMsg)
	ticking  bool // a tick is scheduled
	path     int  // welcome screen: pathSetup or pathAudit
	home     []AreaStatus
	homeCur  int
	Saved    bool // set when the model quit after saving
	// AuditChosen is set when the owner chose "Prepare for an audit" and
	// started it; Run then calls Options.Audit.
	AuditChosen bool
	Err         error
}

var themeCycle = []string{"dark", "light", "plain"}

// New builds the model. It opens the draft with the keychain when the
// machine has one; otherwise it asks for a passphrase first.
func New(opt Options) *Model {
	m := &Model{opt: opt, steps: Steps(opt), w: 80, h: 24, d: draft.New()}
	m.th = ThemeFor(opt.Theme)
	for i, n := range themeCycle {
		if n == m.th.Name {
			m.themeIdx = i
		}
	}
	m.store = &draft.Store{Path: opt.DraftPath}
	switch {
	case opt.Keys != nil:
		m.store.Keys = opt.Keys
		m.unlock()
	default:
		avail := opt.Keychain
		if avail == nil {
			avail = draft.KeychainAvailable
		}
		kdf, _ := m.store.KDF()
		if kdf != "scrypt" && avail() {
			m.store.Keys = draft.Keychain{Service: "box", Account: "onboarding-draft"}
			m.unlock()
		} else {
			m.phase = phaseUnlock
			m.newDraft = !m.store.Exists()
			m.pass = textinput.New()
			m.pass.EchoMode = textinput.EchoPassword
			m.pass.EchoCharacter = '•'
			m.pass.CharLimit = 128
			m.pass.Width = 40
			m.pass.Focus()
		}
	}
	return m
}

// unlock loads the draft with the store's key, or starts a new one.
func (m *Model) unlock() bool {
	if m.store.Exists() {
		d, err := m.store.Load()
		if err != nil {
			m.setErr(err)
			if errors.Is(err, draft.ErrWrongKey) {
				m.status = "That passphrase does not open your saved answers. Try again."
			}
			return false
		}
		m.d = d
		m.status = "Welcome back: your saved answers are loaded."
		m.isErr = false
	}
	m.phase = phaseWizard
	if m.d.Step == "" && len(m.d.Answers) == 0 {
		m.phase = phaseWelcome
	}
	for i, s := range m.steps {
		if s.ID() == m.d.Step {
			m.cur = i
		}
	}
	m.syncCur()
	return true
}

// syncCur moves the current screen's cursor to its saved answer.
func (m *Model) syncCur() {
	if sy, ok := m.steps[m.cur].(interface{ sync(*draft.Draft) }); ok {
		sy.sync(m.d)
	}
}

func (m *Model) setErr(err error) {
	m.status, m.isErr = err.Error(), true
}

func (m *Model) save() {
	m.d.Step = m.steps[m.cur].ID()
	if err := m.store.Save(m.d); err != nil {
		m.setErr(fmt.Errorf("could not save your answers: %w", err))
	}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	if m.phase == phaseUnlock {
		return textinput.Blink
	}
	return m.animate()
}

// animating reports whether something on screen moves.
func (m *Model) animating() bool {
	if m.th.Plain {
		return false
	}
	if m.phase == phaseWelcome {
		return true
	}
	if a, ok := m.steps[m.cur].(interface{ Animating() bool }); ok && m.phase == phaseWizard {
		return a.Animating()
	}
	return false
}

// animate schedules the next frame when something moves and none is pending.
func (m *Model) animate() tea.Cmd {
	if m.ticking || !m.animating() {
		return nil
	}
	m.ticking = true
	return tick()
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		if !m.helpSet {
			m.help = m.w >= 72
		}
		return m, nil
	case tickMsg:
		m.ticking = false
		m.frame++
		if t, ok := m.steps[m.cur].(interface{ Tick(*draft.Draft) }); ok && m.phase == phaseWizard {
			t.Tick(m.d)
		}
		return m, m.animate()
	case tea.KeyMsg:
		var model tea.Model
		var cmd tea.Cmd
		switch m.phase {
		case phaseUnlock:
			model, cmd = m.updateUnlock(msg)
		case phaseWelcome:
			model, cmd = m.updateWelcome(msg)
		case phaseAudit:
			model, cmd = m.updateAudit(msg)
		case phaseHome:
			model, cmd = m.updateHome(msg)
		default:
			model, cmd = m.updateWizard(msg)
		}
		return model, tea.Batch(cmd, m.animate())
	}
	return m, nil
}

func (m *Model) updateWelcome(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc", "q":
		return m, tea.Quit
	case "up", "k", "shift+tab":
		m.path = pathSetup
	case "down", "j", "tab":
		m.path = pathAudit
	case "enter", " ":
		if m.path == pathAudit {
			m.phase = phaseAudit
			return m, nil
		}
		m.phase = phaseWizard
		m.status = "Let's go! Answers save as you move between steps."
	case "ctrl+t":
		m.themeIdx = (m.themeIdx + 1) % len(themeCycle)
		m.th = ThemeFor(themeCycle[m.themeIdx])
	}
	return m, nil
}

func (m *Model) updateUnlock(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		return m, tea.Quit
	case "enter":
		secret := m.pass.Value()
		if len(secret) < 8 {
			m.pass.Reset()
			m.setErr(errors.New("use at least 8 characters"))
			return m, nil
		}
		m.store.Keys = draft.Passphrase{Secret: secret}
		if m.unlock() {
			m.pass.Reset()
			if m.newDraft {
				m.save()
				m.status, m.isErr = "Your answers will be saved encrypted with this passphrase.", false
			}
		} else {
			m.pass.Reset()
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.pass, cmd = m.pass.Update(msg)
	return m, cmd
}

func (m *Model) updateWizard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status, m.isErr = "", false
	switch msg.String() {
	case "ctrl+c", "ctrl+s":
		m.save()
		m.Saved = !m.isErr
		return m, tea.Quit
	case "tab", "ctrl+n":
		if err := m.steps[m.cur].Ready(m.d); err != nil {
			m.setErr(err)
			return m, nil
		}
		if m.cur < len(m.steps)-1 {
			m.cur++
			m.save()
			m.syncCur()
		}
		return m, nil
	case "shift+tab", "ctrl+p":
		if m.cur > 0 {
			m.cur--
			m.save()
			m.syncCur()
		}
		return m, nil
	case "?":
		m.help, m.helpSet = !m.help, true
		return m, nil
	case "ctrl+t":
		m.themeIdx = (m.themeIdx + 1) % len(themeCycle)
		m.th = ThemeFor(themeCycle[m.themeIdx])
		m.status = "Theme: " + m.th.Name
		return m, nil
	}
	if b, ok := m.steps[m.cur].(*buildScreen); ok && b.ended && msg.String() == "enter" && m.opt.Home != nil {
		m.openHome()
		return m, nil
	}
	cmd := m.steps[m.cur].Update(msg, m.d)
	if msg.String() == "enter" || msg.String() == " " {
		m.save()
	}
	return m, cmd
}

// View implements tea.Model.
func (m *Model) View() string {
	w, h := m.w, m.h
	if w < 40 {
		w = 40
	}
	if h < 12 {
		h = 12
	}
	if m.phase == phaseUnlock {
		return fit(m.viewUnlock(w), w, h)
	}
	if m.phase == phaseWelcome {
		v := m.welcomeCentered(w, h)
		top := (h - strings.Count(v, "\n") - 1) / 2
		if top > 0 {
			v = strings.Repeat("\n", top) + v
		}
		return fit(v, w, h)
	}
	if m.phase == phaseAudit {
		return fit(m.viewAudit(w), w, h)
	}
	if m.phase == phaseHome {
		return fit(m.viewHome(w, h), w, h)
	}
	th := m.th
	s := m.steps[m.cur]
	sep := strings.Repeat("─", w)
	if th.Plain {
		sep = strings.Repeat("-", w)
	}
	frac := float64(m.cur) / float64(len(m.steps)-1)
	right := fmt.Sprintf("Step %d of %d · %s", m.cur+1, len(m.steps), s.Title())
	left := "▣ Company in a Box"
	if th.Plain {
		left = "Company in a Box"
	}
	barW := w - lipgloss.Width(left) - lipgloss.Width(right) - 4 - 5
	bar := ""
	if barW >= 8 {
		bar = "  " + th.Bar(min(barW, 30), frac) + th.Muted.Render(fmt.Sprintf(" %3d%%", int(frac*100+0.5)))
	}
	gap := w - lipgloss.Width(left) - lipgloss.Width(bar) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	header := th.Gradient(left, 0, true) + bar + strings.Repeat(" ", gap) + th.Accent.Render(right)
	if th.Plain {
		header = th.Title.Render(left) + bar + strings.Repeat(" ", gap) + th.Accent.Render(right)
	}
	progress := m.progress(w)
	footer := m.keyBar(w)
	status := ""
	if m.status != "" {
		if m.isErr {
			status = th.Err.Render("✗ " + m.status)
		} else {
			status = th.OK.Render(m.status)
		}
	}
	used := 1 + strings.Count(progress, "\n") + 1 + 1 + 1 + 1 + 1
	bodyH := h - used
	if bodyH < 3 {
		bodyH = 3
	}
	body := m.body(w, bodyH)
	out := strings.Join([]string{header, progress, sep, body, sep, fit(status, w, 1), footer}, "\n")
	return fit(out, w, h)
}

func (m *Model) progress(w int) string {
	var items []string
	for i, s := range m.steps {
		mark := m.th.TodoMark
		switch {
		case i < m.cur:
			mark = m.th.DoneMark
		case i == m.cur:
			mark = m.th.CurMark
		}
		item := mark + " " + s.Title()
		if i == m.cur {
			item = m.th.Accent.Render(item)
		} else {
			item = m.th.Step.Render(item)
		}
		items = append(items, item)
	}
	var lines []string
	line := ""
	for _, it := range items {
		if line != "" && lipgloss.Width(line)+2+lipgloss.Width(it) > w {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += "  "
		}
		line += it
	}
	return strings.Join(append(lines, line), "\n")
}

func (m *Model) body(w, h int) string {
	s := m.steps[m.cur]
	title := m.th.Title.Render(s.Title())
	if m.th.Plain || !m.help || w < 72 {
		main := title + "\n\n" + s.View(w, h-2, m.th, m.d)
		if m.help {
			main += "\n\n" + m.th.Accent.Render("What is this?") + "\n" + wrap(s.Help(), w)
		}
		return fit(main, w, h)
	}
	mainW := w*3/5 - 1
	helpW := w - mainW - 3
	main := fit(title+"\n\n"+s.View(mainW, h-2, m.th, m.d), mainW, h)
	help := fit(m.th.Accent.Render(m.helpTitle())+"\n\n"+wrap(s.Help(), helpW), helpW, h)
	ml, hl := strings.Split(main, "\n"), strings.Split(help, "\n")
	var b strings.Builder
	for i := 0; i < h; i++ {
		l := ml[i] + strings.Repeat(" ", max(0, mainW-lipgloss.Width(ml[i])))
		b.WriteString(l + " " + m.th.Muted.Render("│") + " " + hl[i])
		if i < h-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m *Model) helpTitle() string {
	if m.th.Plain {
		return "What is this?"
	}
	return "✦ What is this?"
}

func (m *Model) keyBar(w int) string {
	keys := append([]Key{}, m.steps[m.cur].Keys()...)
	keys = append(keys, Key{"tab", "next"}, Key{"shift+tab", "back"}, Key{"?", "help"}, Key{"ctrl+t", "theme"}, Key{"ctrl+s", "save & quit"})
	var parts []string
	for _, k := range keys {
		parts = append(parts, m.th.Key.Render(k.Keys)+" "+k.Does)
	}
	return fit(strings.Join(parts, " · "), w, 1)
}

func (m *Model) viewUnlock(w int) string {
	var b strings.Builder
	b.WriteString(m.th.Title.Render("Company in a Box") + "\n\n")
	if m.newDraft {
		b.WriteString(wrap("Your answers are saved on this computer, encrypted, until the build stores them in your Microsoft 365 organisation. This computer has no password store, so choose a passphrase (at least 8 characters). You will need it to continue later.", w) + "\n\n")
		b.WriteString("Choose a passphrase:\n")
	} else {
		b.WriteString(wrap("Welcome back. Enter the passphrase you chose to open your saved answers.", w) + "\n\n")
		b.WriteString("Passphrase:\n")
	}
	b.WriteString(m.pass.View() + "\n\n")
	if m.status != "" {
		st := m.th.OK
		if m.isErr {
			st = m.th.Err
		}
		b.WriteString(st.Render(m.status) + "\n\n")
	}
	b.WriteString(m.th.Muted.Render("enter continue · esc quit"))
	return b.String()
}

// Run starts the TUI on the terminal.
func Run(opt Options) error {
	m := New(opt)
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}
	fm, ok := final.(*Model)
	if ok && fm.Saved {
		fmt.Println("Your answers are saved. Run box again to continue where you left off.")
	}
	if ok && fm.AuditChosen && opt.Audit != nil {
		opt.Audit()
	}
	return nil
}
