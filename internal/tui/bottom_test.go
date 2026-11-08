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
	if strings.Contains(stripCodes(m.View()), "ctrl+o") {
		t.Error("the keys are listed before anyone asked")
	}
	if !strings.Contains(stripCodes(m.View()), "? keys") {
		t.Error("nothing says ? shows the keys")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	view := stripCodes(m.View())
	if !strings.Contains(view, "ctrl+o tool rows") || m.input.Value() != "" {
		t.Errorf("? with an empty box did not list the keys:\n%s", view)
	}
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 50 {
			t.Errorf("row %d is %d wide on a 50-wide screen", i, w)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if strings.Contains(stripCodes(m.View()), "ctrl+o") {
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

// a question sits in its own box between the rule and the prompt, which stays visible
func TestAQuestionHasItsOwnBoxAboveThePrompt(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(confirmMsg{ev: agent.Event{Kind: agent.Confirm, Text: "write src/a.go?"}, reply: make(chan agent.Decision, 1)})
	area := stripCodes(m.bottomArea())
	question, prompt := strings.Index(area, "write src/a.go?"), strings.Index(area, promptMark)
	if question < 0 || prompt < 0 || question > prompt {
		t.Errorf("the question is not above the prompt:\n%s", area)
	}
	box := stripCodes(m.question())
	if !strings.Contains(box, "y yes · a always · n no") || !strings.Contains(box, "╭") {
		t.Errorf("the question is not in a box with its answers:\n%s", box)
	}
	if boxWidth := lipgloss.Width(strings.Split(box, "\n")[0]); boxWidth >= 80 {
		t.Errorf("the box spans the screen instead of fitting its question: %q", box)
	}
}

// the footer's two rows sit above the rule, the prompt under it, with a blank row between
func TestTheFooterSitsAboveTheRuleAndThePrompt(t *testing.T) {
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
	project, costs, ruled, prompt := at("/home/user/project (main)"), at("ctx "), at("─"), at(promptMark)
	if project < 0 || costs != project+1 || rows[project+1] == "" {
		t.Errorf("the costs are not the row under the project:\n%s", strings.Join(rows, "\n"))
	}
	if ruled != costs+2 || rows[costs+1] != "" {
		t.Errorf("the rule is not two rows under the costs, across a blank row:\n%s", strings.Join(rows, "\n"))
	}
	if prompt < ruled {
		t.Errorf("the prompt sits above the rule:\n%s", strings.Join(rows, "\n"))
	}
}

// the input takes one row for a short prompt and grows with the lines typed
func TestTheInputGrowsWithWhatIsTyped(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	short := m.view.Height
	if m.input.Height() != 1 {
		t.Fatalf("an empty input is %d rows", m.input.Height())
	}
	m.input.SetValue("one\ntwo\nthree")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
	if m.input.Height() != 3 || m.view.Height != short-2 {
		t.Errorf("three lines: input %d rows, transcript %d (was %d)", m.input.Height(), m.view.Height, short)
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
	if m.view.Height < 3 {
		t.Errorf("the session is left with %d rows to read", m.view.Height)
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
