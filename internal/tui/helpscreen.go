package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// helpScreen is /help: the commands and what each does. The command prints them as text that scrolls away
// with the conversation; here they stay put, narrow to the one you half-remember, and enter puts it in
// the prompt box, spelled out and ready to run or to finish typing.
type helpScreen struct {
	chooser
	commands []agent.CommandSpec
	put      func(command string)
}

func (m *model) openHelp() {
	s := &helpScreen{
		chooser: newChooser("help", "commands the session answers",
			"enter writes it in the box · type to narrow · ↑/↓ moves · esc closes"),
		commands: agent.Commands(),
		put:      func(command string) { m.input.SetValue(command) },
	}
	s.load(s.helpRows())
	m.picker = s
}

func (s *helpScreen) helpRows() []listRow {
	width := 0
	for _, c := range s.commands {
		width = max(width, len(strings.TrimSpace(c.Name+" "+c.Args)))
	}
	rows := make([]listRow, len(s.commands))
	for i, c := range s.commands {
		// the command keeps the front of the row: what it does is the part a short terminal gives up,
		// and the pane below says all of it
		rows[i] = listRow{
			id:   c.Name,
			mark: markIf(c.Waits, "⏳"),
			lead: strings.TrimSpace(c.Name + " " + c.Args),
			text: c.Note,
			hay:  c.Name + " " + c.Note + " " + c.Also,
		}
	}
	return rows
}

// helpDetail is what the command does, and what choosing it would mean.
func (s *helpScreen) helpDetail(id string) []string {
	for _, c := range s.commands {
		if c.Name != id {
			continue
		}
		lines := []string{strings.TrimSpace(c.Name + " " + c.Args), c.Note}
		if c.Also != "" {
			lines = append(lines, "also "+c.Also)
		}
		if c.Waits {
			lines = append(lines, "waits for the turn in flight")
		}
		return append(lines, styleDim.Render("enter puts it in the prompt box"))
	}
	return nil
}

func (s *helpScreen) pick(id string) (tea.Cmd, overlayNext) {
	if id == "" {
		return nil, stayOpen
	}
	s.put(id)
	return nil, closeOverlay
}

func (s *helpScreen) keys(msg tea.Msg) (tea.Cmd, overlayNext, bool) {
	return s.chooser.keys(msg, s.pick)
}

func (s *helpScreen) view(width, height int) string {
	return s.chooser.view(width, height, s.helpDetail)
}

// markIf marks a row only when the condition says it is that kind of row.
func markIf(on bool, mark string) string {
	if on {
		return mark
	}
	return ""
}
