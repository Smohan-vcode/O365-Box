package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// The home screen's "Requests waiting for you" card (T14-18): staff ask for
// a Microsoft Edge extension, and the owner approves or turns each one down
// here (SPEC §25: the box suggests, the owner decides). Options.Requests
// reads the waiting requests; Options.DecideRequest records the decision
// (box extensions approve or reject do the same from the command line).

// ExtensionRequest is one waiting request on the card.
type ExtensionRequest struct {
	Number      int    // the request's number in the ExtensionRequests list
	ExtensionID string // 32 letters a to p
	Label       string // the store's name, or the requester's description
	Requester   string
	Reason      string
	AskEveryone bool // the requester asks for it to be installed for everyone
}

// RequestDecision is the owner's answer to a request.
type RequestDecision struct {
	Number   int
	Approve  bool
	Everyone bool   // approved to be installed for everyone
	Reason   string // why it is turned down (required then)
}

func (m *Model) loadRequests() {
	m.reqs, m.reqErr = nil, ""
	if m.opt.Requests == nil {
		return
	}
	rs, err := m.opt.Requests()
	if err != nil {
		m.reqErr = "Requests could not be read: " + err.Error()
		return
	}
	m.reqs = rs
}

// homeRows is the number of rows the home cursor moves over: the areas,
// then the waiting requests.
func (m *Model) homeRows() int { return len(m.home) + len(m.reqs) }

// curRequest is the request under the cursor, or nil when it is on an area.
func (m *Model) curRequest() *ExtensionRequest {
	i := m.homeCur - len(m.home)
	if i < 0 || i >= len(m.reqs) {
		return nil
	}
	return &m.reqs[i]
}

func (m *Model) decide(d RequestDecision) {
	if m.opt.DecideRequest == nil {
		m.status = "Deciding requests is not available here: use box extensions approve or reject."
		return
	}
	msg, err := m.opt.DecideRequest(d)
	if err != nil {
		m.setErr(err)
		return
	}
	m.status, m.isErr = msg, false
	m.loadRequests()
	if m.homeCur >= m.homeRows() {
		m.homeCur = max(0, m.homeRows()-1)
	}
}

// updateRequestKey handles a key while the cursor is on a request. It
// reports whether it used the key.
func (m *Model) updateRequestKey(msg tea.KeyMsg) bool {
	r := m.curRequest()
	if r == nil {
		return false
	}
	switch msg.String() {
	case "a":
		m.decide(RequestDecision{Number: r.Number, Approve: true})
	case "i":
		m.decide(RequestDecision{Number: r.Number, Approve: true, Everyone: true})
	case "x":
		m.rejecting = r.Number
		m.reason = textinput.New()
		m.reason.Placeholder = "why (the person who asked sees this)"
		m.reason.CharLimit = 300
		m.reason.Width = 50
		m.reason.Focus()
		m.status = fmt.Sprintf("Turn down request %d: type the reason, then enter (esc to cancel).", r.Number)
	default:
		return false
	}
	return true
}

// updateReason handles keys while the owner types a rejection reason.
func (m *Model) updateReason(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.rejecting = 0
		m.status, m.isErr = "Nothing recorded.", false
		return m, nil
	case "enter":
		reason := strings.TrimSpace(m.reason.Value())
		if reason == "" {
			m.status, m.isErr = "Give a reason: the person who asked sees it.", true
			return m, nil
		}
		n := m.rejecting
		m.rejecting = 0
		m.decide(RequestDecision{Number: n, Reason: reason})
		return m, nil
	}
	var cmd tea.Cmd
	m.reason, cmd = m.reason.Update(msg)
	return m, cmd
}

// viewRequests is the card's text.
func (m *Model) viewRequests(w int) string {
	th := m.th
	if m.opt.Requests == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n" + th.Title.Render(fmt.Sprintf("Requests waiting for you (%d)", len(m.reqs))) + "\n")
	if m.reqErr != "" {
		b.WriteString(th.Err.Render(wrap(m.reqErr, w)) + "\n")
	}
	if len(m.reqs) == 0 && m.reqErr == "" {
		b.WriteString(th.Muted.Render(wrap("No extension requests. Staff ask for one with the short form in the Admin site.", w)) + "\n")
	}
	for i, r := range m.reqs {
		mark := "  "
		on := len(m.home)+i == m.homeCur
		if on {
			mark = th.Cursor + " "
		}
		head := fmt.Sprintf("#%d %s", r.Number, r.Label)
		row := mark + head
		if on {
			row = th.Selected.Render(mark + head)
		}
		b.WriteString(fit(row, w, 1) + "\n")
		ask := r.Requester + " asks: " + r.Reason
		if r.AskEveryone {
			ask += " (asks to install it for everyone)"
		}
		b.WriteString(th.Muted.Render(fit("    "+ask, w, 1)) + "\n")
		if on {
			b.WriteString(th.Muted.Render(fit("    extension id "+r.ExtensionID, w, 1)) + "\n")
		}
	}
	if m.rejecting != 0 {
		b.WriteString("  Reason: " + m.reason.View() + "\n")
	}
	return b.String()
}
