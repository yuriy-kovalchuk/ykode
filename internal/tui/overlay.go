package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// place is an overlay over the whole terminal: the box centred, the session covered behind it
func place(width, height int, body string) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, overlayBox.Width(framed(overlayBox, boxWidth(width))).Render(body))
}

// boxWidth is the frame an overlay draws at: narrower than the terminal, so it reads as a box
func boxWidth(width int) int { return min(max(width-6, 30), 100) }

// framed is a width that leaves out the border, as lipgloss v1 measured it, since v2 counts the border in
func framed(box lipgloss.Style, width int) int { return width + box.GetHorizontalBorderSize() }

// overlayInner is what an overlay's text can use: that box less its border and its padding.
func overlayInner(width int) int { return boxWidth(width) - 4 }
