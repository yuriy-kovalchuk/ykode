package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.pending) > 0 {
		switch msg.String() {
		case "y", "Y":
			m.reply(agent.Allow)
		case "a", "A":
			m.reply(agent.AllowAlways)
		case "n", "N", "esc", "enter", "ctrl+c":
			m.reply(agent.Refuse)
		}
		m.refresh()
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		if m.running {
			m.cancelRun()
			return m, nil
		}
		return m, tea.Quit
	case "esc":
		if m.suggest != nil {
			m.dismissSuggestions() // a list on the screen is dismissed before a run is stopped
			m.refresh()
			return m, nil
		}
		if m.running {
			m.cancelRun()
			return m, nil
		}
	case "ctrl+o":
		m.expanded = !m.expanded
		m.refresh()
		return m, nil
	case "ctrl+_", "ctrl+/":
		// the list has to open while a prompt is half typed, which ? cannot do; ctrl+/ arrives as
		// byte 0x1f, which bubbletea names ctrl+_
		m.showKeys = !m.showKeys
		m.refresh()
		return m, nil
	case "ctrl+t":
		// the wheel and the terminal's own selection cannot both have the mouse
		m.mouseOn = !m.mouseOn
		m.refresh()
		if m.mouseOn {
			return m, tea.EnableMouseCellMotion
		}
		return m, tea.DisableMouse
	case "pgup":
		m.view.PageUp()
		return m, nil
	case "pgdown":
		m.view.PageDown()
		m.seenIfAtBottom()
		return m, nil
	case "ctrl+u":
		m.view.HalfPageUp()
		return m, nil
	case "ctrl+d":
		m.view.HalfPageDown()
		m.seenIfAtBottom()
		return m, nil
	case "tab":
		m.completeCommand()
		m.refresh()
		return m, nil
	case "enter":
		m.suggest = nil
		return m.submit()
	}

	switch msg.String() {
	case "ctrl+p", "ctrl+n":
		m.walkHistory(msg.String() == "ctrl+p")
		m.trackSuggestions()
		m.refresh()
		return m, nil
	}

	if m.input.Value() == "" {
		switch msg.String() {
		case "?":
			m.showKeys = !m.showKeys
			m.refresh()
			return m, nil
		case "end":
			m.view.GotoBottom()
			m.unseen = false
			return m, nil
		}
	}

	// a trackpad or wheel in a terminal that is not holding the mouse arrives as arrow keys, so
	// the arrows move the session: a gesture must never write an old prompt into the box. With
	// more than one line in the box there is a caret to move, and it gets the arrows instead.
	if m.input.LineCount() <= 1 {
		switch msg.String() {
		case "up":
			m.view.ScrollUp(wheelStep)
			return m, nil
		case "down":
			m.view.ScrollDown(wheelStep)
			m.seenIfAtBottom()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.fitInput()
	m.trackSuggestions()
	m.layout()
	return m, cmd
}

// walkHistory steps through the prompts sent this session. What was typed but never sent waits at
// the end of the walk, so reading earlier prompts cannot lose the one being written.
func (m *model) walkHistory(older bool) {
	if m.histIdx >= len(m.history) || m.input.Value() != m.history[m.histIdx] {
		m.unsent = m.input.Value() // the box holds a line being written, not a prompt it stopped on
	}
	if older && m.histIdx > 0 {
		m.histIdx--
	} else if !older && m.histIdx < len(m.history) {
		m.histIdx++
	}
	if m.histIdx < len(m.history) {
		m.input.SetValue(m.history[m.histIdx])
		return
	}
	m.histIdx = len(m.history)
	m.input.SetValue(m.unsent)
}

// reply answers the question on the screen; the run that asked it is blocked until then.
func (m *model) reply(d agent.Decision) {
	if len(m.pending) == 0 {
		return
	}
	m.pending[0].reply <- d
	m.pending = m.pending[1:]
}
