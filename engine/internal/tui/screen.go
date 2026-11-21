package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Smohan-vcode/O365-Box/engine/internal/draft"
)

// Key is one entry of the key bar.
type Key struct{ Keys, Does string }

// Screen is one step of the wizard. Screens keep their answers in the
// draft, so a resumed session shows the same values.
type Screen interface {
	ID() string
	Title() string
	// Help is the "What is this?" text for the help pane.
	Help() string
	Keys() []Key
	Update(msg tea.KeyMsg, d *draft.Draft) tea.Cmd
	View(width, height int, th Theme, d *draft.Draft) string
	// Ready returns an error that keeps the user on this screen.
	Ready(d *draft.Draft) error
}
