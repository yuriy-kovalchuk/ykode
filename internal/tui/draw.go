package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// refresh keeps a reader who scrolled up in place and marks the output arriving below.
func (m *model) refresh() {
	atBottom := m.view.AtBottom()
	m.layout()
	content := m.log.render(m.width, m.expanded)
	if content != m.rendered && !atBottom && m.rendered != "" {
		m.unseen = true
	}
	m.rendered = content
	m.view.SetContent(content)
	if atBottom {
		m.view.GotoBottom()
		m.unseen = false
	}
}

// layout shrinks the transcript, because the question, the keys and the input can all grow.
func (m *model) layout() {
	m.view.Height = max(m.height-lipgloss.Height(m.bottomArea()), 3)
}

func (m *model) View() string {
	return lipgloss.JoinVertical(lipgloss.Left, m.view.View(), m.bottomArea())
}

func rule(width int) string {
	return "\x1b[2m" + strings.Repeat("─", max(width, 0)) + "\x1b[0m"
}
