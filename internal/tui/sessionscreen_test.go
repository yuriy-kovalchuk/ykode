package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
)

// ownID, pickedID and foreignID are the three conversations a picker test starts with: this run's, one
// of this project that went before it, and one another project wrote.
const (
	ownID     = "s20260101-120000.000"
	pickedID  = "s20260101-110000.000"
	foreignID = "s20260101-100000.000"
)

// pickerModel is a screen whose run saves a conversation of its own and reads a folder holding two
// more, the way a run that has been going a while does.
func pickerModel(t *testing.T) (*model, string) {
	t.Helper()
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	saveConversation(t, sessions, ownID, dir, "what this run is doing")
	saveConversation(t, sessions, pickedID, dir, "fix the flaky timer test")
	saveConversation(t, sessions, foreignID, "/somewhere/else", "another project's ask")

	own := session.New(sessions, ownID, dir)
	a := agent.New(llm.NewClient("http://127.0.0.1:1/v1", ""), nil, "test-model",
		agent.Options{Session: own, Root: dir}, nil)
	m := newModel(context.Background(), a, NewHub(), config.Config{
		Root: dir, Session: ownID, SessionDir: sessions, SessionFile: own.Path(),
	})
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 40})
	return m, sessions
}

// saveConversation writes one that a resume would read back: an ask and what it got.
func saveConversation(t *testing.T, dir, id, root, ask string) {
	t.Helper()
	log := session.New(dir, id, root)
	for _, said := range []llm.Message{{Role: "user", Content: ask}, {Role: "assistant", Content: "done"}} {
		if err := log.Append(said); err != nil {
			t.Fatal(err)
		}
	}
}

// openList is /sessions typed at the prompt.
func openList(t *testing.T, m *model) string {
	t.Helper()
	m.input.SetValue("/sessions")
	m.Update(enter)
	if m.picker == nil {
		t.Fatal("/sessions did not open the list")
	}
	return stripCodes(m.View())
}

// rowHolding is the first list row that says what it is looking for, so the mark beside a conversation
// can be checked without naming where in the box it was drawn.
func rowHolding(view, want string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	return ""
}

// the list names the conversations by what they were about, newest first, and says which is which
func TestSessionsOpensTheListOfSavedConversations(t *testing.T) {
	m, _ := pickerModel(t)
	view := openList(t, m)

	for _, want := range []string{"ykode · sessions", "3 conversations", "fix the flaky timer test",
		"another project's ask", "this is the conversation this run is in", "esc closes"} {
		if !strings.Contains(view, want) {
			t.Errorf("the list lacks %q:\n%s", want, view)
		}
	}
	if strings.Index(view, pickedID) > strings.Index(view, foreignID) {
		t.Error("the newest conversation is not first")
	}
	if row := rowHolding(view, "what this run is doing"); !strings.Contains(row, "@") {
		t.Errorf("this run's conversation is not marked out: %q", row)
	}
	if row := rowHolding(view, "another project's ask"); !strings.Contains(row, "·") {
		t.Errorf("another project's conversation is not told apart: %q", row)
	}
}

// typing narrows the list to the conversations that could be what you are looking for
func TestTypingNarrowsTheList(t *testing.T) {
	m, _ := pickerModel(t)
	openList(t, m)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("timer")})

	view := stripCodes(m.View())
	if !strings.Contains(view, "fix the flaky timer test") {
		t.Errorf("the conversation that matches was hidden:\n%s", view)
	}
	if strings.Contains(view, foreignID) {
		t.Errorf("a conversation that does not match is still listed:\n%s", view)
	}
}

// enter puts the conversation under the cursor in this run's place: the model reads it, the screen
// draws it, and the run goes on saving to its file
func TestEnterContinuesTheConversationUnderTheCursor(t *testing.T) {
	m, sessions := pickerModel(t)
	openList(t, m)
	m.Update(down)
	if view := stripCodes(m.View()); !strings.Contains(view, "enter continues it") {
		t.Errorf("the row that can be continued does not say so:\n%s", view)
	}
	m.Update(enter)

	if m.picker != nil {
		t.Fatal("the list stayed open after a conversation was continued")
	}
	if got := m.agent.Conversation(); len(got) != 3 || got[1].Content != "fix the flaky timer test" {
		t.Errorf("the run reads %d messages, the second being %q", len(got), got[min(1, len(got)-1)].Content)
	}
	if m.agent.SessionFile() != filepath.Join(sessions, pickedID+".jsonl") {
		t.Errorf("the run goes on saving to %q", m.agent.SessionFile())
	}
	view := stripCodes(m.View())
	for _, want := range []string{"came back to " + pickedID, "fix the flaky timer test", "done",
		"what this run was doing stays saved"} {
		if !strings.Contains(view, want) {
			t.Errorf("%q missing from the screen that came back:\n%s", want, view)
		}
	}
	if strings.Contains(view, "what this run is doing") {
		t.Errorf("the conversation that was given up is still on the screen:\n%s", view)
	}
	if m.cfg.Session != pickedID || filepath.Base(m.cfg.SessionFile) != pickedID+".jsonl" {
		t.Errorf("the run still belongs to %s: file %s", m.cfg.Session, m.cfg.SessionFile)
	}
	if len(m.history) != 1 || m.history[0] != "fix the flaky timer test" {
		t.Errorf("ctrl+p walks %q, not the asks of the conversation now being read", m.history)
	}
}

// esc gives the session back with nothing switched
func TestEscClosesTheListWithoutContinuingAnything(t *testing.T) {
	m, _ := pickerModel(t)
	openList(t, m)
	m.Update(esc)

	if m.picker != nil {
		t.Fatal("esc left the list on the screen")
	}
	if got := m.agent.Conversation(); len(got) != 1 || m.cfg.Session != ownID {
		t.Errorf("closing the list changed the run: %d messages, session %s", len(got), m.cfg.Session)
	}
	if view := stripCodes(m.View()); strings.Contains(view, "ykode · sessions") {
		t.Errorf("the session is still hidden:\n%s", view)
	}
}

// a conversation written in another project cannot be continued here: the list says where it can be
func TestAnotherProjectsConversationIsNotContinued(t *testing.T) {
	m, _ := pickerModel(t)
	openList(t, m)
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m.Update(enter)

	if m.picker == nil {
		t.Fatal("the list closed on a conversation this run cannot continue")
	}
	if got := m.agent.Conversation(); len(got) != 1 {
		t.Errorf("the run reads %d messages after a refused pick", len(got))
	}
	want := "ykode --continue " + foreignID + " --root /somewhere/else"
	if view := stripCodes(m.View()); !strings.Contains(view, want) {
		t.Errorf("the list does not say how to continue it there, wanting %q:\n%s", want, view)
	}
}

// continuing a conversation waits for the turn in flight: it would replace the history a request is
// sending, so the list is not even opened over it
func TestSessionsWaitsForTheTurnToFinish(t *testing.T) {
	m, _ := pickerModel(t)
	m.running = true
	m.input.SetValue("/sessions")
	m.submit()

	if m.picker != nil {
		t.Error("the list opened over a turn that is running")
	}
	if view := stripCodes(m.View()); !strings.Contains(view, "press esc to stop this one first") {
		t.Errorf("a running turn was not told why there is no list:\n%s", view)
	}
}

// a narrow terminal still gets the list: the box stops at its edges and the detail stays
func TestTheListFitsASmallTerminal(t *testing.T) {
	m, _ := pickerModel(t)
	m.Update(tea.WindowSizeMsg{Width: 46, Height: 12})
	openList(t, m)

	view := stripCodes(m.View())
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 46 {
			t.Errorf("a row runs past the terminal (%d columns): %q", lipgloss.Width(line), line)
		}
	}
	for _, want := range []string{"ykode · sessions", "2 msgs", "fix the", "esc closes"} {
		if !strings.Contains(view, want) {
			t.Errorf("a small screen loses %q:\n%s", want, view)
		}
	}

	m.Update(down)
	m.Update(enter)
	if got := m.agent.Conversation(); len(got) != 3 {
		t.Errorf("a pick in a small terminal read %d messages", len(got))
	}
}

// with nothing saved the command says so instead of opening an empty box
func TestSessionsWithNothingSavedSaysSo(t *testing.T) {
	m := newTestModel()
	m.cfg = config.Config{SessionDir: filepath.Join(t.TempDir(), "sessions")}
	m.input.SetValue("/sessions")
	m.submit()

	if m.picker != nil {
		t.Error("an empty folder opened an empty list")
	}
	if view := stripCodes(m.View()); !strings.Contains(view, "nothing saved yet") {
		t.Errorf("an empty sessions folder answered:\n%s", view)
	}
}
