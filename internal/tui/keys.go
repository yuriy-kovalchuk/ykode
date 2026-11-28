package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// handleKey sends a key or a paste to its owner: question, session keys, scrolling, then the box
func (m *model) handleKey(msg tea.Msg) (tea.Model, tea.Cmd) {
	key := "" // a paste is no key: it only ever reaches the typing box
	if press, ok := msg.(tea.KeyPressMsg); ok {
		key = press.String()
	}
	switch {
	case len(m.pending) > 0 && !m.awaitingWords():
		m.answer(key) // a call to allow or refuse answers itself: y, a, n
		m.refresh()
		return m, nil
	case key == "enter":
		m.suggest = nil
		if m.awaitingWords() {
			m.answerWords() // the model asked in words, so the box is what answers
			return m, nil
		}
		return m.submit()
	}
	if key == "ctrl+v" && len(m.pending) == 0 {
		return m, m.pastePicture() // a picture on the clipboard is saved and named; text falls through to a paste
	}
	if cmd, ok := m.sessionKey(key); ok {
		return m, cmd
	}
	if m.scrollKey(key) {
		return m, nil
	}
	if m.holdPaste(msg) {
		return m, nil // a long paste is one token in the box: its lines would slow every key after it
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

// sessionKey handles the keys acting on the session, not the text; the bool says it was one
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
		// the list must open while a prompt is half typed, which ? cannot; ctrl+/ arrives as ctrl+_
		m.toggleKeys()
	case "ctrl+o":
		m.planFolded = !m.planFolded
		m.refresh()
	case "ctrl+r":
		m.log.showWhole(!m.log.whole) // a command cut to its row is read whole, then folded back
		m.log.rows(m.width)           // drawn now, so a reader scrolled up is not told of new output
		m.refresh()
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

// scrollKey moves the transcript: a wheel arrives as arrows, which must not write history in
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

// walkHistory steps through the prompts sent, with the unsent draft waiting at the end
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
