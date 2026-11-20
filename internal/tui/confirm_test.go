package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// /reset throws away the conversation, so one keystroke may not do it: the screen asks first
func TestResetAsksBeforeForgettingTheConversation(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 92, Height: 24})
	m.input.SetValue("/reset")
	m.submit()

	if m.picker == nil {
		t.Fatal("/reset went through without asking")
	}
	if view := stripCodes(m.render()); !strings.Contains(view, "forget the conversation") {
		t.Errorf("the question does not say what is at stake:\n%s", view)
	}

	m.Update(typedKeys("n"))
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
