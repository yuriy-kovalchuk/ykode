package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// typeInto presses the keys a user would, so the list is reached through the input, not set up.
func typeInto(t *testing.T, m *model, keys string) {
	t.Helper()
	for _, r := range keys {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// a slash at the prompt shows what it could still become, with what each one does
func TestTypingASlashListsWhatItCouldStillBe(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 90, 24
	typeInto(t, m, "/th")

	out := stripCodes(m.View())
	if !strings.Contains(out, "/think [level]") || !strings.Contains(out, "how deeply the model thinks") {
		t.Errorf("the candidates are not on the screen:\n%s", out)
	}
	if strings.Contains(out, "/tools") {
		t.Errorf("a command that no longer fits is still listed:\n%s", out)
	}
}

// tab types what the candidates agree on, and leaves the argument to be typed
func TestTabTypesWhatTheCandidatesShare(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 90, 24
	typeInto(t, m, "/t")
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // /think, /tools, /think... share only the slash and t
	if m.input.Value() != "/t" {
		t.Errorf("tab invented a command: %q", m.input.Value())
	}
	typeInto(t, m, "o")
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // only /tools is left
	if m.input.Value() != "/tools" {
		t.Errorf("tab completed to %q, want /tools", m.input.Value())
	}

	m2 := newTestModel()
	m2.width, m2.height = 90, 24
	typeInto(t, m2, "/mod")
	m2.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m2.input.Value() != "/model " {
		t.Errorf("tab completed to %q, want /model with room for the id", m2.input.Value())
	}
}

// esc clears the list and keeps the line; typing brings the list back
func TestEscClearsTheListAndKeepsTheLine(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 90, 24
	typeInto(t, m, "/th")
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	out := stripCodes(m.View())
	if strings.Contains(out, "how deeply the model thinks") {
		t.Errorf("esc left the list on the screen:\n%s", out)
	}
	if m.input.Value() != "/th" {
		t.Errorf("esc changed the line: %q", m.input.Value())
	}
	typeInto(t, m, "i")
	if !strings.Contains(stripCodes(m.View()), "how deeply the model thinks") {
		t.Error("the list did not come back on the next key")
	}
}

// a short screen still sees that more could match
func TestALongListSaysHowManyMoreThereAre(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 90, 40
	typeInto(t, m, "/")

	out := stripCodes(m.View())
	if !strings.Contains(out, "more, keep typing") {
		t.Errorf("the list did not say it was cut short:\n%s", out)
	}
	if rows := strings.Count(out, "  /"); rows > 7 {
		t.Errorf("%d command rows shown, want the list capped:\n%s", rows, out)
	}
}

// the candidates sit between the session and the prompt, and the session keeps its rows
func TestTheListSitsAboveThePromptWithoutHidingTheSession(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 90, 20
	m.log.answer("the change goes in the status line, drawn per frame")
	m.refresh() // what an answer event does: the session is drawn before the typing starts
	typeInto(t, m, "/ctx")

	rows := strings.Split(stripCodes(m.View()), "\n")
	list, prompt, session := -1, -1, -1
	for i, row := range rows {
		if strings.Contains(row, "how full the window is") {
			list = i
		}
		if strings.Contains(row, promptMark) {
			prompt = i
		}
		if strings.Contains(row, "drawn per frame") {
			session = i
		}
	}
	if list < 0 || prompt < 0 || list > prompt {
		t.Errorf("the list is not above the prompt (row %d vs %d):\n%s", list, prompt, strings.Join(rows, "\n"))
	}
	if session < 0 {
		t.Errorf("the list pushed the session off the screen:\n%s", strings.Join(rows, "\n"))
	}
}
