package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// refresh keeps a reader who scrolled up in place and marks the output arriving below.
func (m *model) refresh() {
	m.redrawQueued = false
	atBottom := m.view.atBottom()
	m.layout()
	arriving := m.log.hasPending()
	m.view.setRows(m.log.rows(m.width, m.expanded))
	if arriving && !atBottom {
		m.unseen = true
	}
	if atBottom {
		m.view.gotoBottom()
		m.unseen = false
	}
}

// layout shrinks the transcript, because the question, the keys and the input can all grow. A
// reader at the end of the session stays there: the box growing must not lift them off it.
func (m *model) layout() {
	atBottom := m.view.atBottom()
	m.view.height = max(m.height-lipgloss.Height(m.bottomArea()), 3)
	if atBottom {
		m.view.gotoBottom()
	}
}

func (m *model) View() string {
	// stacked by hand: both blocks already stop at the screen's edge, and JoinVertical measures
	// every row of both again to find that out
	return m.view.view() + "\n" + m.bottomArea()
}

func rule(width int) string {
	return styleDim.Render(strings.Repeat("─", max(width, 0)))
}
