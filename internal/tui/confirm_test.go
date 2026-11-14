package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// /reset throws away the whole conversation, so one keystroke is not allowed to do it: the screen asks
// first, and no says no
func TestResetAsksBeforeForgettingTheConversation(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 92, Height: 24})
	m.input.SetValue("/reset")
	m.submit()

	if m.picker == nil {
		t.Fatal("/reset went through without asking")
	}
	if view := stripCodes(m.View()); !strings.Contains(view, "forget the conversation") {
		t.Errorf("the question does not say what is at stake:\n%s", view)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.picker != nil {
		t.Error("the answer left the question on the screen")
	}
	if out := m.log.render(92); strings.Contains(out, "history cleared") {
		t.Errorf("no forgot the conversation anyway:\n%s", out)
	}
}

// yes runs the command, and its answer lands in the transcript as if it had been typed
func TestResetAfterYesForgetsTheConversation(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 92, Height: 24})
	m.input.SetValue("/reset")
	m.submit()
	m.Update(enter)

	if m.picker != nil {
		t.Error("the box stayed up after the conversation was forgotten")
	}
	if out := m.log.render(92); !strings.Contains(out, "history cleared") {
		t.Errorf("the reset was not reported:\n%s", out)
	}
}

// esc leaves it alone, which is the same answer as no
func TestEscLeavesTheConversationAlone(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 92, Height: 24})
	m.input.SetValue("/reset")
	m.submit()
	m.Update(esc)

	if m.picker != nil {
		t.Error("esc left the question on the screen")
	}
	if out := m.log.render(92); strings.Contains(out, "history cleared") {
		t.Errorf("esc forgot the conversation:\n%s", out)
	}
}

// /undo force overwrites what is in the file now, so it asks too: but only when there is a change to put
// back, which is what the command itself says when there is not
func TestUndoForceAsksWhenThereIsSomethingToPutBack(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 92, Height: 24})
	m.input.SetValue("/undo force")
	m.submit()

	if m.picker != nil {
		t.Error("the screen asked about a change that was never made")
	}
	if out := m.log.render(92); !strings.Contains(out, "nothing to undo") {
		t.Errorf("a command with nothing to undo did not say so:\n%s", out)
	}
}
