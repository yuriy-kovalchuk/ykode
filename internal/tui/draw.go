package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// refresh keeps a scrolled-up reader in place and marks output arriving below, before rows are built
func (m *model) refresh() {
	m.undrawn, m.drawn = false, time.Now()
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

// topRows is a blank row under the terminal's top edge, taken off the session's height
const topRows = 1

// layout shrinks the transcript, since question, keys and input grow; a reader at the end stays
func (m *model) layout() {
	atBottom := m.view.atBottom()
	m.view.height = max(m.height-lipgloss.Height(m.screenBottom())-topRows, 3)
	if atBottom {
		m.view.gotoBottom()
	}
}

// screenBottom is everything under the session, built once per message for layout and for View
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
	if m.picker != nil {
		return m.picker.view(m.width, m.height)
	}
	// stacked by hand: both blocks already stop at the edge, so JoinVertical would remeasure them
	return strings.Repeat("\n", topRows) + m.view.view() + "\n" + m.screenBottom()
}
