package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeText(m *model, text string) {
	for _, r := range text {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// boxRows is what the box shows, row by row, without the styles and the padding the box adds.
func boxRows(m *model) []string {
	rows := strings.Split(stripCodes(m.input.View()), "\n")
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	return rows
}

func inputModel(width int) *model {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
	return m
}

const sentence = "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron pi rho sigma tau"

// a long line wraps onto new rows and the box grows to hold them: the start of what was typed stays
// in the box at every keystroke, instead of scrolling out of sight above it
func TestTheStartOfWhatIsTypedStaysVisibleAsTheLineWraps(t *testing.T) {
	m := inputModel(40)
	for i, r := range sentence {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if rows := boxRows(m); !strings.HasPrefix(sentence, rows[0]) {
			t.Fatalf("after %d characters the first row of the box is %q, which is not where the text starts\n%s", i+1, rows[0], strings.Join(rows, "\n"))
		}
	}
	if rows := boxRows(m); len(rows) != 3 || !strings.HasPrefix(rows[2], "omicron") {
		t.Errorf("the box is %d rows: %q", len(rows), rows)
	}
}

// every row of the text is in the box, none of it above the top and no blank row at the bottom
func TestTheBoxHoldsExactlyTheRowsOfTheText(t *testing.T) {
	m := inputModel(40)
	typeText(m, sentence)
	joined := strings.Join(strings.Fields(strings.Join(boxRows(m), " ")), " ")
	if joined != sentence {
		t.Errorf("the box shows %q, want the whole text %q", joined, sentence)
	}
	if got, want := m.input.Height(), 3; got != want {
		t.Errorf("the box is %d rows tall, want %d", got, want)
	}
}

// a paste of several lines shows all of them, up to the most the box grows to
func TestAPasteOfSeveralLinesIsAllVisible(t *testing.T) {
	m := inputModel(60)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("one\ntwo\nthree\nfour\nfive"), Paste: true})
	rows := boxRows(m)
	if len(rows) != 5 || rows[0] != "one" || rows[4] != "five" {
		t.Errorf("a five line paste shows %q", rows)
	}
}

// erasing back across the rows the line wrapped onto shrinks the box and keeps the start in view
func TestErasingBackAcrossWrappedRowsKeepsTheStartInView(t *testing.T) {
	m := inputModel(40)
	typeText(m, sentence)
	for i := 0; i < len(sentence)-3; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		if rows := boxRows(m); !strings.HasPrefix(sentence, rows[0]) {
			t.Fatalf("after erasing %d characters the first row is %q, which is not where the text starts", i+1, rows[0])
		}
	}
	if rows := boxRows(m); len(rows) != 1 || rows[0] != "alp" {
		t.Errorf("what is left shows as %q", rows)
	}
}

// typing in the middle of the text so that it wraps further down keeps the start in view
func TestTypingInTheMiddleOfTheTextKeepsTheStartInView(t *testing.T) {
	m := inputModel(40)
	typeText(m, "alpha beta gamma delta epsilon zeta eta theta")
	for i := 0; i < 20; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	}
	typeText(m, " inserted words that push the end onto another row")
	if rows := boxRows(m); !strings.HasPrefix(rows[0], "alpha") {
		t.Errorf("after typing in the middle the first row is %q:\n%s", rows[0], strings.Join(rows, "\n"))
	}
}

// past the most the box grows to it scrolls, with the cursor's row in view, and it comes back whole
// when the text fits again
func TestAVeryLongTextScrollsAndComesBackWholeWhenItFitsAgain(t *testing.T) {
	m := inputModel(40)
	long := strings.Repeat("word ", 90) // about eleven or twelve rows at this width
	typeText(m, "start "+long+"end")
	if got := m.input.Height(); got != maxInputLines {
		t.Fatalf("the box is %d rows, want it held at %d", got, maxInputLines)
	}
	if rows := boxRows(m); !strings.HasSuffix(strings.Join(rows, " "), "end") {
		t.Errorf("the end of the text is not in view: %q", rows)
	}
	for i := 0; i < len(long)+3; i++ { // erase most of it
		m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if rows := boxRows(m); !strings.HasPrefix(rows[0], "start") {
		t.Errorf("with the text short again, the first row is %q: the start is scrolled out of the box", rows[0])
	}
}

// walking back to an earlier prompt that took several rows shows it from its start
func TestAnEarlierPromptOfSeveralRowsComesBackFromItsStart(t *testing.T) {
	m := inputModel(40)
	m.history = []string{sentence}
	m.histIdx = 1
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if rows := boxRows(m); len(rows) != 3 || !strings.HasPrefix(rows[0], "alpha") {
		t.Errorf("an earlier prompt shows as %q", rows)
	}
}
