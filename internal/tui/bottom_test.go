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

// a question sits in its own box between the transcript and the prompt, which stays visible
func TestAQuestionHasItsOwnBoxAboveThePrompt(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(confirmMsg{ev: agent.Event{Kind: agent.Confirm, Text: "write src/a.go?"}, reply: make(chan agent.Decision, 1)})
	area := stripCodes(m.bottomArea())
	question, prompt := strings.Index(area, "write src/a.go?"), strings.Index(area, promptMark)
	if question < 0 || prompt < 0 || question > prompt {
		t.Errorf("the question is not above the prompt:\n%s", area)
	}
	if !strings.Contains(area, "y yes · a always · n no") || !strings.Contains(area, "╭") {
		t.Errorf("the question is not in a box with its answers:\n%s", area)
	}
	if box := strings.Split(area, "\n")[0]; lipgloss.Width(box) >= 80 {
		t.Errorf("the box spans the screen instead of fitting its question: %q", box)
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
