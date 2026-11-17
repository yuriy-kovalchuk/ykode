package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// some commands answer with a box over the session; lists and reports share one slot and keys
type overlay interface {
	view(width, height int) string
	// keys answers the screen's keys, saying whether it closed or stayed open with what it learned
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

// place is an overlay over the whole terminal: the box centred, the session covered behind it
func place(width, height int, body string) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, overlayBox.Width(boxWidth(width)).Render(body))
}

// boxWidth is the frame an overlay draws at: narrower than the terminal, so it reads as a box
func boxWidth(width int) int { return min(max(width-6, 30), 100) }

// overlayInner is what an overlay's text can use: that box less its border and its padding.
func overlayInner(width int) int { return boxWidth(width) - 4 }
