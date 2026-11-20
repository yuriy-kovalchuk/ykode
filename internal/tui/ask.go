package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// answerWords takes the box as the answer: a bare number picks a choice, an empty line says none
func (m *model) answerWords() {
	text := strings.TrimSpace(m.input.Value())
	m.input.Reset()
	if choice, err := strconv.Atoi(text); err == nil && choice >= 1 && choice <= len(m.pending[0].options) {
		text = m.pending[0].options[choice-1] // the model reads the choice in words, not the number it was picked by
	}
	m.replyWords(text)
}

// noAnswer answers the open question with nothing, which is what stopping the run does to it
func (m *model) noAnswer() {
	if len(m.pending) > 0 && m.pending[0].words != nil {
		m.replyWords("")
	}
}

// replyWords hands the words to the run that asked and puts the next question, if any, in its place.
func (m *model) replyWords(answer string) {
	q := m.pending[0]
	q.words <- answer
	m.pending = m.pending[1:]
	m.refresh()
}

// awaitingWords says the question wants an answer in words, so the keys belong to the box
func (m *model) awaitingWords() bool {
	return len(m.pending) > 0 && m.pending[0].words != nil
}

// modelQuestion is the model's own question above the box, its choices numbered, and the keys
func (m *model) modelQuestion(screenRows int) string {
	q := m.pending[0]
	room := max(m.width-lipgloss.Width(askBar)-2, 10)
	keys := askKeys(room)
	lines := askWrapped(q.text, room, max(screenRows-len(keys)-1, 1))
	for i, line := range lines {
		lines[i] = styleBold.Render(line)
	}
	// the question and keys keep their rows; a choice with no row left is still answerable by number
	lines = append(lines, optionRows(q.options, room, max(screenRows-len(lines)-len(keys), 1))...)
	lines = append(lines, keys...)
	for i, line := range lines {
		lines[i] = styleKey.Render(askBar) + line
	}
	return strings.Join(lines, "\n")
}

// askKeysLine says how an ask is answered: typing it, or the number of one of its choices.
var askKeysLine = styleDim.Render("type it, or the number of a choice · ") + styleKey.Render("enter") +
	styleDim.Render(" answers · ") + styleKey.Render("esc") + styleDim.Render(" stops the run")

// askKeys is that line as the rows allow: a narrow one reads it as two, not past its edge
func askKeys(width int) []string {
	if lipgloss.Width(askKeysLine) <= width {
		return []string{askKeysLine}
	}
	return []string{
		styleDim.Render("type it, or the number of a choice"),
		styleKey.Render("enter") + styleDim.Render(" answers · ") + styleKey.Render("esc") +
			styleDim.Render(" stops the run"),
	}
}

// askWrapped is text read across rows rather than cut at one, ending in … when rows run out
func askWrapped(text string, width, rows int) []string {
	wrapped := wrap(text, width)
	if len(wrapped) <= rows {
		return wrapped
	}
	kept := wrapped[:rows]
	kept[len(kept)-1] = cutToWidth(kept[len(kept)-1], max(width-1, 1))
	return kept
}

// optionRows are the choices numbered, each over as many rows as it needs, counted when cut
func optionRows(options []string, room, rows int) []string {
	const optionLead = 3 // the number, the gap after it, and the column the text starts at
	var lines []string
	shown := 0
	for i, option := range options {
		parts := wrap(option, max(room-optionLead, 10))
		spare := rows - len(lines)
		if len(parts) > spare {
			if len(lines) > 0 { // the first choice is shown whatever the screen: an ask with nothing of it
				break // on the screen is an ask nobody can read, not one to answer blind
			}
			parts = parts[:max(spare, 1)]
		}
		lines = append(lines, numbered(i, parts, optionLead)...)
		shown = i + 1
		if len(parts) > spare {
			break
		}
	}
	if shown < len(options) {
		lines = append(lines, styleDim.Render(fmt.Sprintf("… %d more, their numbers still answer", len(options)-shown)))
	}
	return lines
}

// numbered is one choice as its rows: the first opens with its number, the rest line up under the text.
func numbered(at int, parts []string, lead int) []string {
	rows := make([]string, 0, len(parts))
	for i, part := range parts {
		if i > 0 {
			rows = append(rows, strings.Repeat(" ", lead)+part)
			continue
		}
		rows = append(rows, styleKey.Render(fmt.Sprintf("%d", at+1))+styleDim.Render(" ")+part)
	}
	return rows
}
