package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// confirm asks once before something that cannot be undone: /reset forgets the conversation the run has
// been building, and /undo force writes a file back over what someone changed there since. It is the same
// box the lists open, with two keys, because the question is one word long.
type confirm struct {
	title, ask string
	yes        func()
}

// confirmCommand puts the question on the screen with the command that answers it: yes runs it exactly as
// typing it would, and its answer lands in the transcript as usual.
func (m *model) confirmCommand(line, title, ask string) {
	m.picker = &confirm{
		title: title,
		ask:   ask,
		yes: func() {
			out, _, err := m.agent.Command(m.ctx, line)
			if out = strings.TrimRight(out, "\n"); out != "" {
				m.log.output(out)
			}
			if err != nil {
				m.err = err.Error()
			}
		},
	}
}

func (c *confirm) keys(msg tea.Msg) (tea.Cmd, overlayNext, bool) {
	key, isKey := msg.(tea.KeyMsg)
	if !isKey {
		return nil, stayOpen, false
	}
	switch key.String() {
	case "y", "Y", "enter":
		c.yes()
		return nil, closeOverlay, true
	case "n", "N", "esc", "ctrl+c":
		return nil, closeOverlay, true
	}
	return nil, stayOpen, true
}

func (c *confirm) view(width, height int) string {
	inner := overlayInner(width)
	lines := []string{
		setupTitle.Render("ykode · " + c.title),
		"",
		styleBold.Render(c.ask),
		"",
		setupDim.Render(shortenHint("y or enter does it · n or esc leaves it", inner)),
	}
	return place(width, height, strings.Join(lines, "\n"))
}
