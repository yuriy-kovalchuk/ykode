package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	promptMark    = "❯ "
	maxInputLines = 6 // the input grows with what is typed, up to this
)

// keys is what ? shows, as key and what it does.
var keys = [][2]string{
	{"enter", "send"}, {"ctrl+j", "newline"}, {"↑ ↓", "earlier prompts"}, {"wheel pgup", "scroll"},
	{"end", "back to the end"}, {"esc", "stop"}, {"ctrl+o", "tool rows"}, {"ctrl+t", "select text"},
	{"ctrl+c", "quit"}, {"?", "hide keys"},
}

var questionBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorWarn).Padding(0, 1)

// bottomArea is everything under the transcript: a question if one waits, the rule, the prompt, the status.
func (m *model) bottomArea() string {
	var rows []string
	if len(m.pending) > 0 {
		rows = append(rows, m.question())
	}
	rows = append(rows, ruleWith(m.width, m.scrollMark()))
	if m.showKeys {
		rows = append(rows, m.keysHelp())
	}
	rows = append(rows, m.promptLines(), statusLine(m.status()))
	if m.err != "" {
		rows = append(rows, styleBad.Render(oneLine(m.err, max(m.width-2, 1))))
	}
	return strings.Join(rows, "\n")
}

// question is the confirmation a run waits on, in a box of its own above the input.
func (m *model) question() string {
	q := m.pending[0]
	lines := []string{styleWarn.Render("⚠ ") + q.text + " " + styleBold.Render("[y/N/a]")}
	if len(q.target) > 0 {
		lines = append(lines, styleWarn.Render("outside the project: "+strings.Join(q.target, ", ")))
	}
	answers := "y yes · a always · n no"
	if len(m.pending) > 1 {
		answers += fmt.Sprintf(" · %d more waiting", len(m.pending)-1)
	}
	lines = append(lines, styleDim.Render(answers))
	body := strings.Join(lines, "\n")
	width := min(lipgloss.Width(body)+2, max(m.width-2, 10)) // the box fits its question, never the screen
	return questionBox.Width(width).Render(body)
}

// promptLines is the input with the prompt mark on its first line.
func (m *model) promptLines() string {
	mark := styleAccent.Render(promptMark)
	if m.running {
		mark = styleDim.Render(promptMark) // typing still works; it waits for the turn
	}
	lines := strings.Split(m.input.View(), "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = mark + lines[i]
		} else {
			lines[i] = strings.Repeat(" ", lipgloss.Width(promptMark)) + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// fitInput grows the input with the lines typed, so a short prompt takes one row.
func (m *model) fitInput() {
	m.input.SetHeight(min(max(m.input.LineCount(), 1), maxInputLines))
}

// keysHelp lists the keys, wrapping between entries so no row is wider than the screen.
func (m *model) keysHelp() string {
	var rows []string
	row, used := "", 0
	for _, k := range keys {
		desc := k[1]
		if k[0] == "ctrl+t" && m.mouseOff {
			desc = "wheel scrolls"
		}
		plain := k[0] + " " + desc
		if used > 0 && used+3+len(plain) > m.width {
			rows, row, used = append(rows, row), "", 0
		}
		if used > 0 {
			row += styleDim.Render(" · ")
			used += 3
		}
		row += styleKey.Render(k[0]) + " " + styleDim.Render(desc)
		used += len(plain)
	}
	return strings.Join(append(rows, row), "\n")
}
