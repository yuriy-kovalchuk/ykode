package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// answerWords takes what is in the box as the answer to the question the model asked: a bare number
// picks one of the choices it offered, and an empty line says there is none. Either way the run
// waiting on the question is told, so it never hangs on a question nobody means to answer.
func (m *model) answerWords() {
	text := strings.TrimSpace(m.input.Value())
	m.input.Reset()
	if choice, err := strconv.Atoi(text); err == nil && choice >= 1 && choice <= len(m.pending[0].options) {
		text = m.pending[0].options[choice-1] // the model reads the choice in words, not the number it was picked by
	}
	m.replyWords(text)
}

// noAnswer answers the question that is open with nothing, which is what stopping the run does to it:
// the model is told to go on and say what it assumed.
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

// awaitingWords says the question on the screen wants an answer in words, so the keys belong to the
// box rather than to a letter or two.
func (m *model) awaitingWords() bool {
	return len(m.pending) > 0 && m.pending[0].words != nil
}

// modelQuestion is the question the model asked, above the box: what it asks in its own words, the
// choices numbered the way they are answered, and the keys. It is not a warning: nothing is being
// allowed or refused, the run is only waiting to be told.
func (m *model) modelQuestion(screenRows int) string {
	q := m.pending[0]
	room := max(m.width-lipgloss.Width(askBar)-2, 10)
	keys := askKeys(room)
	lines := askWrapped(q.text, room, max(screenRows-len(keys)-1, 1))
	for i, line := range lines {
		lines[i] = styleBold.Render(line)
	}
	// the question and the keys keep their rows; what is left is for the choices, and a choice with no
	// row left is still answerable by its number
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

// askKeys is that line as the rows the terminal has for it: a narrow one reads it as two, at the gap
// between the two answers, rather than running one row past its edge.
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

// askWrapped is text that says more than fits in a row: it is read across rows rather than cut at one,
// and only runs out of rows on a screen too small for it, where the last row shown ends in ….
func askWrapped(text string, width, rows int) []string {
	wrapped := wrap(text, width)
	if len(wrapped) <= rows {
		return wrapped
	}
	kept := wrapped[:rows]
	kept[len(kept)-1] = cutToWidth(kept[len(kept)-1], max(width-1, 1))
	return kept
}

// optionRows are the choices numbered, each across as many rows as it needs: a choice longer than the
// terminal is still one you can read, and answer by its number, rather than a fragment of one. The
// choices no row is left for are counted instead, which is what they were before.
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
