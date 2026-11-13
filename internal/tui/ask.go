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
	lines := []string{styleBold.Render(oneLine(q.text, room))}
	// the question and the keys keep their rows; what is left is for the choices, and a choice that
	// did not fit is still answerable by its number
	shown := min(len(q.options), max(screenRows-2, 1))
	for i, option := range q.options[:shown] {
		lines = append(lines, styleKey.Render(fmt.Sprintf("%d", i+1))+styleDim.Render(" ")+cutToWidth(option, room))
	}
	if shown < len(q.options) {
		lines = append(lines, styleDim.Render(fmt.Sprintf("… %d more, their numbers still answer", len(q.options)-shown)))
	}
	keys := styleDim.Render("type it, or the number of a choice · ") + styleKey.Render("enter") +
		styleDim.Render(" answers · ") + styleKey.Render("esc") + styleDim.Render(" stops the run")
	lines = append(lines, keys)
	for i, line := range lines {
		lines[i] = styleKey.Render(askBar) + line
	}
	return strings.Join(lines, "\n")
}
