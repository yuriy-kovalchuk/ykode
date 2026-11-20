package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// suggestions are the commands the line could still become, while it is still one name long
func suggestions(typed string) []agent.CommandSpec {
	if !strings.HasPrefix(typed, "/") || strings.ContainsAny(typed, " \n") {
		return nil
	}
	var list []agent.CommandSpec
	for _, c := range agent.Commands() {
		spelled := spellingFor(c, typed)
		if spelled == "" {
			continue
		}
		// the candidate keeps the spelling being typed, so tab finishes the word the line started
		if spelled != c.Name {
			c.Name = spelled
		}
		list = append(list, c)
	}
	return list
}

// spellingFor is the name the typed line is typing; a bare / lists each command once
func spellingFor(c agent.CommandSpec, typed string) string {
	if strings.HasPrefix(c.Name, typed) {
		return c.Name
	}
	if c.Also != "" && strings.HasPrefix(c.Also, typed) {
		return c.Also
	}
	return ""
}

// trackSuggestions re-asks unless esc cleared the list, which stays cleared until typing again
func (m *model) trackSuggestions() {
	if m.suggestFor == m.input.Value() {
		return
	}
	m.suggestFor = m.input.Value()
	m.suggest = suggestions(m.suggestFor)
}

// completeCommand types what the candidates share; one candidate becomes the whole command
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

// suggestRows show what is being chosen, up to the rows spare, and how many more wait
func (m *model) suggestRows(room int) []string {
	if room <= 0 {
		return nil // a screen with nothing to spare keeps the rows for the prompt and the session
	}
	count, shorter := len(m.suggest), false
	if count > room {
		count, shorter = room-1, true // the row that counts what is left is one of the room
	}
	column := 0
	names := make([]string, 0, count)
	for _, c := range m.suggest[:count] {
		names = append(names, strings.TrimSpace(c.Name+" "+c.Args))
		column = max(column, len(names[len(names)-1]))
	}
	var rows []string
	for i, c := range m.suggest[:count] {
		lead := "  " + styleKey.Render(fmt.Sprintf("%-*s", column, names[i])) + " "
		columns := m.width - lipgloss.Width(lead) - 2 // the columns it takes, not the bytes its colour does
		rows = append(rows, lead+styleDim.Render(cutToWidth(c.Note, max(columns, 1))))
	}
	if shorter {
		rows = append(rows, styleDim.Render(fmt.Sprintf("  … %d more, keep typing", len(m.suggest)-count)))
	}
	return rows
}
