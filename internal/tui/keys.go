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
		// a captured mouse cannot select text, so the user can hand it back to the terminal
		m.mouseOff = !m.mouseOff
		m.refresh()
		if m.mouseOff {
			return m, tea.DisableMouse
		}
		return m, tea.EnableMouseCellMotion
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
	case "enter":
		return m.submit()
	}

	// with an empty box the arrows walk the prompts you sent; with text they move the cursor
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
		case "up":
			if m.histIdx > 0 {
				m.histIdx--
				m.input.SetValue(m.history[m.histIdx])
			}
			return m, nil
		case "down":
			if m.histIdx < len(m.history)-1 {
				m.histIdx++
				m.input.SetValue(m.history[m.histIdx])
			} else {
				m.histIdx = len(m.history)
				m.input.SetValue("")
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.fitInput()
	m.layout()
	return m, cmd
}

// reply answers the question on the screen; the run that asked it is blocked until then.
func (m *model) reply(d agent.Decision) {
	if len(m.pending) == 0 {
		return
	}
	m.pending[0].reply <- d
	m.pending = m.pending[1:]
}
