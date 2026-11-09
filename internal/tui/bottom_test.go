package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// the keys stay out of the way until ? asks for them, and then wrap rather than overflow
func TestQuestionMarkShowsTheKeys(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 24})
	if strings.Contains(stripCodes(m.View()), "ctrl+t") {
		t.Error("the keys are listed before anyone asked")
	}
	if !strings.Contains(stripCodes(m.View()), "? keys") {
		t.Error("nothing says ? shows the keys")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	view := stripCodes(m.View())
	if !strings.Contains(view, "ctrl+t terminal selects") || m.input.Value() != "" {
		t.Errorf("? with an empty box did not list the keys:\n%s", view)
	}
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 50 {
			t.Errorf("row %d is %d wide on a 50-wide screen", i, w)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if strings.Contains(stripCodes(m.View()), "ctrl+t") {
		t.Error("a second ? did not hide the keys")
	}
}

// with text in the box, ? is just a character
func TestQuestionMarkInAPromptIsTyped(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("why")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if m.input.Value() != "why?" || m.showKeys {
		t.Errorf("input %q, keys shown %v: ? was taken as a command", m.input.Value(), m.showKeys)
	}
}

// a question sits in a bar of its own between the rule and the prompt, which stays visible
func TestAQuestionHasItsOwnBarAboveThePrompt(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	args := `{"path":"src/a.go","content":"package a\nfunc A() {}\n"}`
	m.Update(confirmMsg{ev: agent.Event{Kind: agent.Confirm, Tool: "write_file", Args: args, Text: "write_file"}, reply: make(chan agent.Decision, 1)})
	area := stripCodes(m.bottomArea())
	question, prompt := strings.Index(area, "src/a.go"), strings.Index(area, promptMark)
	if question < 0 || prompt < 0 || question > prompt {
		t.Errorf("the question is not above the prompt:\n%s", area)
	}
	box := stripCodes(m.question())
	for _, want := range []string{"write_file", "src/a.go · 2 lines", "func A() {}", "y yes · a always · n no"} {
		if !strings.Contains(box, want) {
			t.Errorf("the question lacks %q:\n%s", want, box)
		}
	}
	for _, row := range strings.Split(box, "\n") {
		if !strings.HasPrefix(row, askBar) {
			t.Errorf("a row of the question is outside its bar: %q", row)
		}
		if lipgloss.Width(row) >= 80 {
			t.Errorf("a row of the question spans the screen: %q", row)
		}
	}
	if strings.Contains(box, `{"`) {
		t.Errorf("the question shows the call's JSON:\n%s", box)
	}
}

// a call whose arguments run past what the screen once kept still reads as words, not as JSON
func TestALongWriteIsAskedAboutInWords(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	args := `{"path":"data/t.txt","content":"` + strings.Repeat("hello world\\n", 100) + `"}`
	m.Update(confirmMsg{ev: agent.Event{Kind: agent.Confirm, Tool: "write_file", Args: args}, reply: make(chan agent.Decision, 1)})
	box := stripCodes(m.question())
	if !strings.Contains(box, "data/t.txt · 100 lines") || strings.Contains(box, `{"content"`) {
		t.Errorf("a long write is not described:\n%s", box)
	}
	if !strings.Contains(box, "… 94 more lines") {
		t.Errorf("the preview does not say what it left out:\n%s", box)
	}
}

// the footer's two rows sit above the rule, the prompt under it, with a blank row between
// what the session reports sits under the prompt: the prompt is where the reader stops reading,
// and the two rows of numbers are what is read last
func TestTheReportSitsUnderThePrompt(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.root, m.branch = "/home/user/project", "main"
	rows := strings.Split(stripCodes(m.bottomArea()), "\n")
	at := func(needle string) int {
		for i, row := range rows {
			if strings.Contains(row, needle) {
				return i
			}
		}
		return -1
	}
	ruled, prompt := at("\u2500"), at(promptMark)
	project, costs := at("/home/user/project (main)"), at("ctx ")
	if ruled < 0 || prompt < ruled {
		t.Errorf("the prompt does not sit under the rule:\n%s", strings.Join(rows, "\n"))
	}
	if project < prompt || costs != project+1 {
		t.Errorf("the report is not two rows under the prompt:\n%s", strings.Join(rows, "\n"))
	}
	if rows[project-1] != "" {
		t.Errorf("no row of air between the prompt and what it reports:\n%s", strings.Join(rows, "\n"))
	}
	if costs != len(rows)-2 { // the last row is the gap under the cursor
		t.Errorf("the report is not the last thing on the screen:\n%s", strings.Join(rows, "\n"))
	}
}

// the input takes one row for a short prompt and grows with the lines typed
func TestTheInputGrowsWithWhatIsTyped(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	short := m.view.height
	if m.input.Height() != 1 {
		t.Fatalf("an empty input is %d rows", m.input.Height())
	}
	m.input.SetValue("one\ntwo\nthree")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
	if m.input.Height() != 3 || m.view.height != short-2 {
		t.Errorf("three lines: input %d rows, transcript %d (was %d)", m.input.Height(), m.view.height, short)
	}
}

// the cursor is not squeezed against the bottom of the terminal
func TestThePromptHasARowOfAirBelowIt(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("count the lines")
	rows := strings.Split(m.bottomArea(), "\n")
	if rows[len(rows)-1] != "" {
		t.Errorf("the last row of the screen holds the cursor: %q", rows[len(rows)-1])
	}
}

// a prompt typed as one long line still takes the rows it wraps over
func TestTheBoxGrowsWithWrappedRowsToo(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.input.SetValue(strings.Repeat("word ", 40))
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
	if m.input.Height() < 3 {
		t.Errorf("a prompt wrapping over several rows leaves the box at %d rows", m.input.Height())
	}
}

// past ten rows the box keeps its height and scrolls what is typed inside itself
func TestTheBoxHoldsTenRowsAndScrollsTheRest(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.input.SetValue("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
	if m.input.Height() != maxInputLines {
		t.Errorf("twelve rows typed: the box holds %d rows, want %d", m.input.Height(), maxInputLines)
	}
}

// a long prompt on a short screen may not push the session off it: the screen stays the terminal
func TestALongPromptOnAShortScreenKeepsTheSessionVisible(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 14})
	m.log.answer("the file holds 1,204 lines")
	m.input.SetValue("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
	if got := len(strings.Split(m.View(), "\n")); got != 14 {
		t.Errorf("the screen is %d rows, want the 14 it was given", got)
	}
	if m.view.height < 3 {
		t.Errorf("the session is left with %d rows to read", m.view.height)
	}
}

// whatever is on the screen, the screen stays the size of the terminal
func TestTheKeysHelpDoesNotPushTheFramePastTheTerminal(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 22})
	m.input.SetValue("1\n2\n3\n4\n5\n6\n7\n8\n9\n10")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if m.input.Height() != maxInputLines {
		t.Fatalf("the box holds %d rows, want %d", m.input.Height(), maxInputLines)
	}
	if got := len(strings.Split(m.View(), "\n")); got != 22 {
		t.Errorf("the frame is %d rows on a 22 row terminal: keys help pushed it past the screen", got)
	}
}

// the list names what the app answers, scrolling chords included
func TestTheKeysListNamesTheChordsInTheUse(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.showKeys = true
	list := stripCodes(m.keysHelp())
	for _, chord := range []string{"ctrl+u", "ctrl+d", "pgup", "ctrl+/", "ctrl+j", "tab", "ctrl+t"} {
		if !strings.Contains(list, chord) {
			t.Errorf("%s is answered but not listed: %s", chord, list)
		}
	}
}

// an ask waiting for the next round-trip keeps its row above the prompt, however few rows the
// screen has, and the session keeps its rows too
func TestAWaitingAskKeepsItsRowAboveThePrompt(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 12
	m.running = true
	m.agent.Steer("and the tests too")

	rows := strings.Split(stripCodes(m.View()), "\n")
	waiting, prompt := -1, -1
	for i, row := range rows {
		if strings.Contains(row, "waits for the next round-trip") {
			waiting = i
		}
		if strings.Contains(row, promptMark) {
			prompt = i
		}
	}
	if waiting < 0 || prompt < 0 || waiting > prompt {
		t.Errorf("the waiting ask is not above the prompt (row %d vs %d):\n%s", waiting, prompt, strings.Join(rows, "\n"))
	}
	if !strings.Contains(strings.Join(rows, "\n"), "and the tests too") {
		t.Error("the waiting ask lost its words")
	}
}

// the keys are laid out by what the terminal shows: the arrows take more bytes than they do columns
func TestTheKeysListStaysInsideANarrowScreen(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 30, Height: 24})
	for _, row := range strings.Split(m.keysHelp(), "\n") {
		if got := lipgloss.Width(stripCodes(row)); got > 30 {
			t.Errorf("a keys row takes %d columns of a 30 column screen: %q", got, stripCodes(row))
		}
	}
}
