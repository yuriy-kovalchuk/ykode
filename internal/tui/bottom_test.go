package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// the keys stay out of the way until ? asks for them, and then wrap rather than overflow
func TestQuestionMarkShowsTheKeys(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 24})
	if strings.Contains(stripCodes(m.render()), "ctrl+o fold") {
		t.Error("the keys are listed before anyone asked")
	}
	if !strings.Contains(stripCodes(m.render()), "? keys") {
		t.Error("nothing says ? shows the keys")
	}
	m.Update(typedKeys("?"))
	view := stripCodes(m.render())
	if !strings.Contains(view, "ctrl+o fold the plan") || m.input.Value() != "" {
		t.Errorf("? with an empty box did not list the keys:\n%s", view)
	}
	for i, line := range strings.Split(m.render(), "\n") {
		if w := lipgloss.Width(line); w > 50 {
			t.Errorf("row %d is %d wide on a 50-wide screen", i, w)
		}
	}
	m.Update(typedKeys("?"))
	if strings.Contains(stripCodes(m.render()), "ctrl+o fold") {
		t.Error("a second ? did not hide the keys")
	}
}

// with text in the box, ? is just a character
func TestQuestionMarkInAPromptIsTyped(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("why")
	m.Update(typedKeys("?"))
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
	box := stripCodes(m.question(m.height)) // the terminal is tall here: the room is not what this test checks
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
	box := stripCodes(m.question(m.height)) // the terminal is tall here: the room is not what this test checks
	if !strings.Contains(box, "data/t.txt · 100 lines") || strings.Contains(box, `{"content"`) {
		t.Errorf("a long write is not described:\n%s", box)
	}
	if !strings.Contains(box, "… 94 more lines") {
		t.Errorf("the preview does not say what it left out:\n%s", box)
	}
}

// the report sits under the prompt: reading stops at the prompt, and the numbers come last
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
	if !strings.HasPrefix(rows[project-1], "╰") {
		t.Errorf("the prompt's card is not closed above what it reports:\n%s", strings.Join(rows, "\n"))
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
	m.Update(typedKeys("!"))
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
	m.Update(typedKeys("!"))
	if m.input.Height() < 3 {
		t.Errorf("a prompt wrapping over several rows leaves the box at %d rows", m.input.Height())
	}
}

// past ten rows the box keeps its height and scrolls what is typed inside itself
func TestTheBoxHoldsTenRowsAndScrollsTheRest(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m.input.SetValue("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12")
	m.Update(typedKeys("!"))
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
	m.Update(typedKeys("!"))
	if got := len(strings.Split(m.render(), "\n")); got != 14 {
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
	m.Update(typedKeys("!"))
	m.Update(typedKeys("?"))
	if m.input.Height() != maxInputLines {
		t.Fatalf("the box holds %d rows, want %d", m.input.Height(), maxInputLines)
	}
	if got := len(strings.Split(m.render(), "\n")); got != 22 {
		t.Errorf("the frame is %d rows on a 22 row terminal: keys help pushed it past the screen", got)
	}
}

// the list names what the app answers, scrolling chords included
func TestTheKeysListNamesTheChordsInTheUse(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.showKeys = true
	list := stripCodes(m.keysHelp())
	for _, chord := range []string{"ctrl+u", "ctrl+d", "pgup", "ctrl+/", "ctrl+j", "tab", "ctrl+o"} {
		if !strings.Contains(list, chord) {
			t.Errorf("%s is answered but not listed: %s", chord, list)
		}
	}
}

// an ask waiting for the next round-trip keeps its row above the prompt, on any screen
func TestAWaitingAskKeepsItsRowAboveThePrompt(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 12
	m.running = true
	m.agent.Steer("and the tests too")

	rows := strings.Split(stripCodes(m.render()), "\n")
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

// the keys come in groups, named in a column on a wide screen and above their keys on a narrow one
func TestTheKeysListIsGroupedByWhatTheKeysAreFor(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	rows := strings.Split(stripCodes(m.keysHelp()), "\n")
	want := []string{"prompt   enter send", "edit     ctrl+a", "scroll   ↑ ↓", "session  esc stop the run"}
	if len(rows) != len(want) {
		t.Fatalf("a wide screen lists the keys in %d rows, want one per group:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	for i, start := range want {
		if !strings.HasPrefix(rows[i], start) {
			t.Errorf("row %d is %q, want it to start %q", i, rows[i], start)
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 30, Height: 30})
	if rows := strings.Split(stripCodes(m.keysHelp()), "\n"); rows[0] != "prompt" || rows[1] != "enter send" {
		t.Errorf("a narrow screen does not put the group's name above its keys:\n%s", strings.Join(rows, "\n"))
	}
}

// a group that wraps is cut into rows of the same length, so no row holds a lone entry
func TestAGroupThatWrapsIsSplitEvenly(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	rows := strings.Split(stripCodes(m.keysHelp()), "\n")
	if rows[0] != "prompt   enter send · ctrl+j alt/ctrl+enter newline" ||
		strings.TrimSpace(rows[1]) != "tab complete a /command · ctrl+p ctrl+n earlier prompts" {
		t.Errorf("the prompt keys are not split two and two:\n%s", strings.Join(rows, "\n"))
	}
}

// the list names every newline key, ctrl+enter among them, on the row that says what they do
func TestTheKeysListNamesCtrlEnterForANewLine(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.Update(tea.KeyPressMsg{Code: '/', Mod: tea.ModCtrl})
	shown := stripCodes(m.render())
	if !strings.Contains(shown, "ctrl+j alt/ctrl+enter newline") {
		t.Errorf("the keys list does not offer ctrl+enter for a new line:\n%s", shown)
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

// the box drawn last frame is reused only while it shows the same thing: a value set without a key shows
func TestTheBoxIsDrawnAgainWhenWhatItShowsChanges(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.render()
	m.input.SetValue("an earlier prompt") // as walking the history does, with no key reaching the box
	m.bottom = ""
	if !strings.Contains(stripCodes(m.render()), "an earlier prompt") {
		t.Error("the box kept its last drawing after its text changed")
	}
	m.input.SetValue("an earlier prompt, longer")
	m.input.SetCursorColumn(3)
	m.bottom = ""
	before := m.boxDrawn
	m.render()
	if m.boxDrawn == before {
		t.Error("the box kept its last drawing after its cursor moved")
	}
}
