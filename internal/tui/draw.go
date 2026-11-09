package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// refresh keeps a reader who scrolled up in place and marks the output arriving below. The mark is
// decided before the rows under the session are built, because it is one of the things they say.
func (m *model) refresh() {
	m.redrawQueued = false
	atBottom := m.view.atBottom()
	if arriving := m.log.hasPending(); arriving && !atBottom {
		m.unseen = true
	} else if atBottom {
		m.unseen = false
	}
	m.fitInput()
	m.layout()
	m.view.setRows(m.log.rows(m.width))
	if atBottom {
		m.view.gotoBottom()
	}
}

// topRows is a blank row between the terminal's top edge and the session, so the first row of an
// answer does not sit against the border. It is taken off the session's height wherever that height
// is worked out, so the screen stays exactly as tall as the terminal.
const topRows = 1

// layout shrinks the transcript, because the question, the keys and the input can all grow. A
// reader at the end of the session stays there: the box growing must not lift them off it.
func (m *model) layout() {
	atBottom := m.view.atBottom()
	m.view.height = max(m.height-lipgloss.Height(m.screenBottom())-topRows, 3)
	if atBottom {
		m.view.gotoBottom()
	}
}

// screenBottom is everything under the session, built once per message: the layout needs its height
// and View draws the same rows, so building it twice measures the whole prompt area again for
// nothing. It is empty between messages, and what is drawn is never empty.
func (m *model) screenBottom() string {
	if m.bottom == "" {
		m.bottom = m.bottomArea()
	}
	return m.bottom
}

func (m *model) View() string {
	if m.modal != nil {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.modal.View())
	}
	if m.screen != nil {
		return m.screen.view(m.width, m.height, m.cfg, m.agent.Model())
	}
	// stacked by hand: both blocks already stop at the screen's edge, and JoinVertical measures
	// every row of both again to find that out
	return strings.Repeat("\n", topRows) + m.view.view() + "\n" + m.screenBottom()
}

func rule(width int) string {
	return styleDim.Render(strings.Repeat("─", max(width, 0)))
}
