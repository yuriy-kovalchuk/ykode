package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The screen answers some commands with something to look at rather than text in the transcript: a
// rounded box filling the terminal, with the session kept behind it. A list to choose from and a report
// to read are both that, so they share one slot and one way to open, be typed into, and close.
type overlay interface {
	view(width, height int) string
	// keys answers the screen's keys, and says what happens after the turn: closed, or open with what
	// it learned kept in it.
	keys(msg tea.Msg) (tea.Cmd, overlayNext, bool)
}

// overlayNext says what the overlay does after answering a key.
type overlayNext int

const (
	stayOpen overlayNext = iota
	closeOverlay
)

// overlayBox is the frame every overlay is drawn in, so a command's list or report reads as one thing.
var overlayBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Padding(0, 2)

// place is an overlay over the session: the whole terminal, so the box is centred and the session
// behind it is covered rather than scrolled out of the transcript.
func place(width, height int, body string) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, overlayBox.Width(boxWidth(width)).Render(body))
}

// boxWidth is the frame an overlay is drawn at: narrower than the terminal, so it reads as a box standing
// on it rather than as the terminal itself.
func boxWidth(width int) int { return min(max(width-6, 30), 100) }

// overlayInner is what an overlay's text can use: that box less its border and its padding.
func overlayInner(width int) int { return boxWidth(width) - 4 }
