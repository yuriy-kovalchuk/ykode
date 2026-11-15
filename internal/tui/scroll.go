package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const wheelStep = 3 // lines one notch of the wheel moves the transcript

// wheel scrolls the transcript when a wheel report reaches us at all. The screen never asks the
// terminal for the pointer, so normally it sends none; a report can still arrive when another
// program left a tracking mode set, and scrolling is the only thing a wheel may do. Anything the
// report is not about - a click, a drag, a release - is dropped here rather than passed on, so no
// pointer event can ever reach the input box as keystrokes.
func (m *model) wheel(msg tea.MouseMsg) {
	if msg.Action != tea.MouseActionPress {
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
	label = styleWarn.Render(" " + label + " ")
	fill := width - lipgloss.Width(label) - 2
	if fill < 1 {
		return rule(width)
	}
	return styleDim.Render(strings.Repeat("─", fill)) + label + styleDim.Render("──")
}
