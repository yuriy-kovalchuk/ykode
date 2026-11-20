package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

const wheelStep = 3 // lines one notch of the wheel moves the transcript

// wheel scrolls the transcript when a report arrives at all; other pointer reports are dropped
func (m *model) wheel(msg tea.MouseMsg) {
	notch, ok := msg.(tea.MouseWheelMsg)
	if !ok {
		return
	}
	switch notch.Button {
	case tea.MouseWheelUp:
		m.view.scroll(-wheelStep)
	case tea.MouseWheelDown:
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
