package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const wheelStep = 3 // lines one notch of the wheel moves the transcript

// wheel scrolls the transcript; only the wheel is used, so a click never moves anything.
func (m *model) wheel(msg tea.MouseMsg) {
	if !m.mouseOn || msg.Action != tea.MouseActionPress {
		return
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.view.scroll(-wheelStep)
	case tea.MouseButtonWheelDown:
		m.view.scroll(wheelStep)
		m.seenIfAtBottom()
	}
}

// seenIfAtBottom clears the "new output below" mark once the reader is back at the end.
func (m *model) seenIfAtBottom() {
	if m.view.atBottom() {
		m.unseen = false
	}
}

// scrollMark is what the rule above the status says while the reader is not at the end.
func (m *model) scrollMark() string {
	if m.view.atBottom() {
		return ""
	}
	mark := fmt.Sprintf("↑ %d%%", int(m.view.percent()*100))
	if m.unseen {
		mark += " · new output below"
	}
	return mark + " · end returns"
}

// ruleWith is the rule with a label at its right end, for what the reader should know.
func ruleWith(width int, label string) string {
	if label == "" {
		return rule(width)
	}
	label = " " + label + " "
	fill := width - lipgloss.Width(label) - 2
	if fill < 1 {
		return rule(width)
	}
	return "\x1b[2m" + strings.Repeat("─", fill) + "\x1b[0m\x1b[33m" + label + "\x1b[0m\x1b[2m──\x1b[0m"
}
