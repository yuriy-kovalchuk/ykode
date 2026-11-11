package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// maxSuggestRows is how many commands fit under the prompt before the rest are counted instead.
const maxSuggestRows = 6

// suggestions are the commands the typed line could still become. Only a line that is one command
// name long has a guess worth showing: once the argument starts, the command is chosen.
func suggestions(typed string) []agent.CommandSpec {
	if !strings.HasPrefix(typed, "/") || strings.ContainsAny(typed, " \n") {
		return nil
	}
	var list []agent.CommandSpec
	for _, c := range agent.Commands() {
		if strings.HasPrefix(c.Name, typed) {
			list = append(list, c)
		}
	}
	return list
}

// trackSuggestions re-asks what the line could become, unless the line has not changed since esc
// cleared the list: then it stays cleared until you type again.
func (m *model) trackSuggestions() {
	if m.suggestFor == m.input.Value() {
		return
	}
	m.suggestFor = m.input.Value()
	m.suggest = suggestions(m.suggestFor)
}

// completeCommand types what the candidates agree on: one candidate becomes the whole command,
// several become as much of it as they share.
func (m *model) completeCommand() {
	if len(m.suggest) == 0 {
		return
	}
	shared := m.suggest[0].Name
	for _, c := range m.suggest[1:] {
		shared = sharedPrefix(shared, c.Name)
	}
	if shared == m.input.Value() {
		return // the candidates disagree exactly where the typing stopped
	}
	if len(m.suggest) == 1 && shared != "" && m.suggest[0].Args != "" {
		shared += " " // the command is settled; the argument is yours
	}
	m.input.SetValue(shared)
	m.fitInput()
	m.trackSuggestions()
}

func sharedPrefix(a, b string) string {
	for i := range a {
		if i >= len(b) || a[i] != b[i] {
			return a[:i]
		}
	}
	return a
}

// dismissSuggestions hides the list until the line changes; the typed text stays as it was.
func (m *model) dismissSuggestions() {
	m.suggest, m.suggestFor = nil, m.input.Value()
}

// suggestRows show what is still being chosen, and how many more are waiting beyond the screen's
// patience.
func (m *model) suggestRows() []string {
	shown := min(len(m.suggest), maxSuggestRows)
	column := 0
	names := make([]string, 0, shown)
	for _, c := range m.suggest[:shown] {
		names = append(names, strings.TrimSpace(c.Name+" "+c.Args))
		column = max(column, len(names[len(names)-1]))
	}
	var rows []string
	for i, c := range m.suggest[:shown] {
		lead := "  " + styleKey.Render(fmt.Sprintf("%-*s", column, names[i])) + " "
		room := m.width - lipgloss.Width(lead) - 2 // the columns it takes, not the bytes its colour does
		rows = append(rows, lead+styleDim.Render(cutToWidth(c.Note, max(room, 1))))
	}
	if len(m.suggest) > shown {
		rows = append(rows, styleDim.Render(fmt.Sprintf("  … %d more, keep typing", len(m.suggest)-shown)))
	}
	return rows
}
