package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// handleKey sends a key to whoever owns it: the question on the screen, then the session's own keys,
// then the scrolling keys, and last the box a prompt is typed in.
func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch {
	case len(m.pending) > 0:
		m.answer(key)
		m.refresh()
		return m, nil
	case key == "enter":
		m.suggest = nil
		return m.submit()
	}
	if cmd, ok := m.sessionKey(key); ok {
		return m, cmd
	}
	if m.scrollKey(key) {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.trackSuggestions()
	m.fitInput() // after the suggestions: the box is sized against the rows the bottom will hold
	m.layout()
	return m, cmd
}

// answer is a key pressed while a question waits: only the three answers count, and n is the default.
func (m *model) answer(key string) {
	switch key {
	case "y", "Y":
		m.reply(agent.Allow)
	case "a", "A":
		m.reply(agent.AllowAlways)
	case "n", "N", "esc", "enter", "ctrl+c":
		m.reply(agent.Refuse)
	}
}

// sessionKey handles the keys that act on the session rather than on the text in the box. The bool
// says the key was one of them.
func (m *model) sessionKey(key string) (tea.Cmd, bool) {
	switch key {
	case "ctrl+c":
		if m.running {
			m.cancelRun()
			return nil, true
		}
		return tea.Quit, true
	case "esc":
		switch {
		case m.suggest != nil:
			m.dismissSuggestions() // a list on the screen is dismissed before a run is stopped
			m.refresh()
		case m.running:
			m.cancelRun()
		default:
			return nil, false
		}
		return nil, true
	case "ctrl+_", "ctrl+/":
		// the list has to open while a prompt is half typed, which ? cannot do; ctrl+/ arrives as
		// byte 0x1f, which bubbletea names ctrl+_
		m.toggleKeys()
	case "ctrl+t":
		// the wheel and the terminal's own selection cannot both have the mouse
		m.mouseOn = !m.mouseOn
		m.refresh()
		if m.mouseOn {
			return tea.EnableMouseCellMotion, true
		}
		return tea.DisableMouse, true
	case "tab":
		m.completeCommand()
		m.refresh()
	case "ctrl+p", "ctrl+n":
		m.walkHistory(key == "ctrl+p")
		m.trackSuggestions()
		m.refresh()
	case "?":
		if m.input.Value() != "" {
			return nil, false // a question mark in a prompt is typed
		}
		m.toggleKeys()
	case "end":
		if m.input.Value() != "" {
			return nil, false
		}
		m.view.gotoBottom()
		m.unseen = false
	default:
		return nil, false
	}
	return nil, true
}

func (m *model) toggleKeys() {
	m.showKeys = !m.showKeys
	m.refresh()
}

// scrollKey moves the transcript. A trackpad or wheel in a terminal that is not holding the mouse
// arrives as arrow keys, so the arrows move the session: a gesture must never write an old prompt into
// the box. With more than one line in the box there is a caret to move, and it gets the arrows instead.
func (m *model) scrollKey(key string) bool {
	rows := 0
	switch key {
	case "pgup":
		rows = -m.pageRows(false)
	case "pgdown":
		rows = m.pageRows(false)
	case "ctrl+u":
		rows = -m.pageRows(true)
	case "ctrl+d":
		rows = m.pageRows(true)
	case "up", "down":
		if m.input.LineCount() > 1 {
			return false
		}
		rows = wheelStep
		if key == "up" {
			rows = -wheelStep
		}
	default:
		return false
	}
	m.view.scroll(rows)
	if rows > 0 {
		m.seenIfAtBottom()
	}
	return true
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

// pageRows is how many rows one page takes: the rows the session has room for, or half of them.
func (m *model) pageRows(half bool) int {
	rows := m.view.height
	if half {
		rows /= 2
	}
	return max(rows, 1)
}
