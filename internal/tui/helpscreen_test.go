package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// window92 is a terminal wide enough for a list to have something to say in.
var window92 = tea.WindowSizeMsg{Width: 92, Height: 24}

// /help lists the commands where they stay put, instead of text that scrolls off with the conversation
func TestHelpListsTheCommands(t *testing.T) {
	m := newTestModel()
	m.Update(window92)
	m.input.SetValue("/help")
	m.submit()

	if m.picker == nil {
		t.Fatal("/help did not open the list")
	}
	view := stripCodes(m.render())
	for _, want := range []string{"ykode · help", "commands the session answers", "/compact", "/sessions"} {
		if !strings.Contains(view, want) {
			t.Errorf("the help list lacks %q:\n%s", want, view)
		}
	}
}

// typing narrows to the half-remembered command, and enter writes it spelled out in the box
func TestHelpWritesTheChosenCommandInThePromptBox(t *testing.T) {
	m := newTestModel()
	m.Update(window92)
	m.input.SetValue("/help")
	m.submit()
	m.Update(typedKeys("compact"))

	if view := stripCodes(m.render()); strings.Contains(view, "/sessions") {
		t.Errorf("the narrowed help list still holds the rest:\n%s", view)
	}
	m.Update(enter)

	if m.picker != nil {
		t.Error("the box stayed up after a command was chosen")
	}
	if got := m.input.Value(); got != "/compact" {
		t.Errorf("the prompt box holds %q, not the command that was picked", got)
	}
}
