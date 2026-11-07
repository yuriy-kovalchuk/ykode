package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

func newTestModel() *model {
	a := agent.New(nil, nil, "test-model", agent.Options{}, nil)
	return newModel(context.Background(), a, NewHub())
}

func TestTypingWhileTheRunWorksQueuesTheMessage(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	m.running = true
	m.input.SetValue("also check the tests")

	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("a queued message should not end the program")
	}
	if len(m.queue) != 1 {
		t.Fatalf("nothing was queued: %v", m.queue)
	}
	if !strings.Contains(m.log.render(80, false), "queued") {
		t.Error("the screen does not say the message is waiting")
	}
}

func TestASlashCommandAnswersWithoutTheModel(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	m.input.SetValue("/usage")
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("/usage should not quit")
	}
	if !strings.Contains(m.log.render(80, false), "nothing asked yet") {
		t.Errorf("/usage answered nothing:\n%s", m.log.render(80, false))
	}

	m.input.SetValue("/exit")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("/exit did not stop the program")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("/exit sent no quit message")
	}
}

func TestTheAnswerTravelsBackToTheRun(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	reply := make(chan agent.Decision, 1)
	m.Update(confirmMsg{ev: agent.Event{Kind: agent.Confirm, Text: "write src/a.go?", Target: []string{"/tmp/a.go"}}, reply: reply})

	if !strings.Contains(m.View(), "[y/N/a]") {
		t.Error("the screen does not ask")
	}
	if !strings.Contains(m.View(), "/tmp/a.go") {
		t.Error("an outside path is not shown")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if got := <-reply; got != agent.AllowAlways {
		t.Errorf("answering 'a' came back as %v", got)
	}
	if len(m.pending) > 0 {
		t.Error("the question is still open after it was answered")
	}
}

func TestRefusingIsTheDefaultAnswer(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyEnter},
		{Type: tea.KeyEscape},
		{Type: tea.KeyRunes, Runes: []rune("n")},
		{Type: tea.KeyCtrlC},
	} {
		m := newTestModel()
		reply := make(chan agent.Decision, 1)
		m.Update(confirmMsg{ev: agent.Event{Kind: agent.Confirm, Text: "run rm -rf?"}, reply: reply})
		m.Update(key)
		select {
		case got := <-reply:
			if got != agent.Refuse {
				t.Errorf("%s answered %v, want a refusal", key, got)
			}
		default:
			t.Errorf("%s left the run waiting", key)
		}
	}
}

func TestCtrlCCancelsARunBeforeItQuits(t *testing.T) {
	m := newTestModel()
	m.running = true
	var cancelled bool
	turnCtx, cancel := context.WithCancel(context.Background())
	_ = turnCtx
	m.cancel = func() { cancelled = true; cancel() }

	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd != nil {
		t.Error("ctrl+c quit while a run was going")
	}
	if !cancelled {
		t.Error("ctrl+c did not cancel the run")
	}

	m.running = false
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Error("ctrl+c should quit once nothing is running")
	}
}

func TestTheArrowsWalkOlderPromptsOnlyWithAnEmptyBox(t *testing.T) {
	m := newTestModel()
	m.history = []string{"first question", "second question"}
	m.histIdx = len(m.history)

	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.input.Value() != "second question" {
		t.Errorf("up gave %q", m.input.Value())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.input.Value() != "second question" {
		t.Errorf("down gave %q", m.input.Value())
	}

	m.input.SetValue("typing")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.input.Value() != "typing" {
		t.Errorf("up overwrote what was being typed: %q", m.input.Value())
	}
}

func TestToolRowsOpenAndClose(t *testing.T) {
	m := newTestModel()
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if !m.expanded {
		t.Error("ctrl+o did not open the tool rows")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if m.expanded {
		t.Error("ctrl+o again did not close them")
	}
}

func TestAScreenThatWasNeverResizedStillDraws(t *testing.T) {
	m := newTestModel()
	m.handle(agent.Event{Kind: agent.Answer, Text: "the answer"})
	out := stripCodes(m.View())
	if !strings.Contains(out, "the answer") || !strings.Contains(out, "test-model") {
		t.Errorf("an unsized terminal drew nothing useful:\n%s", out)
	}
}

func TestACommandThatChangesTheConversationWaitsForTheTurn(t *testing.T) {
	m := newTestModel()
	m.running = true
	m.input.SetValue("/reset")
	m.submit()
	out := m.log.render(80, false)
	if strings.Contains(out, "history cleared") {
		t.Error("/reset cut the conversation out from under the turn")
	}
	if !strings.Contains(out, "waits for the turn") {
		t.Errorf("the screen never said why it refused:\n%s", out)
	}
}

func TestAReadOnlyCommandAnswersDuringATurn(t *testing.T) {
	m := newTestModel()
	m.running = true
	m.input.SetValue("/usage")
	m.submit()
	if !strings.Contains(m.log.render(80, false), "nothing asked yet") {
		t.Errorf("/usage was held up by the turn:\n%s", m.log.render(80, false))
	}
}

func TestLeavingCancelsTheTurnInsteadOfRacingIt(t *testing.T) {
	m := newTestModel()
	m.running = true
	var cancelled bool
	m.cancel = func() { cancelled = true }
	m.input.SetValue("/exit")
	if _, cmd := m.submit(); cmd == nil {
		t.Fatal("/exit did not stop the program")
	}
	if !cancelled {
		t.Error("leaving left the turn running")
	}
}

// newWiredModel is a session whose run reports back to a queue the test feeds to Update,
// the way a running program's event loop would.
// wired is a session plus the queue its run reports into, so a test can be the event loop.

func TestCompactingDoesNotStopTheScreen(t *testing.T) {
	w := newWiredModel(t, "goal: fix the timeout\nfiles: src/a.go\nnext: run the tests")
	w.m.input.SetValue("/compact")
	if _, cmd := w.m.submit(); cmd != nil {
		t.Fatal("compacting should not end the session")
	}
	if !w.m.running {
		t.Error("the screen thinks nothing is happening while the summary is being written")
	}
	if !strings.Contains(w.m.log.render(80, false), "compacting") {
		t.Error("the screen does not say what it is doing")
	}

	deadline := time.Now().Add(10 * time.Second)
	for w.m.running && time.Now().Before(deadline) {
		w.feed()
		time.Sleep(50 * time.Millisecond)
	}
	if w.m.running {
		t.Fatal("the summary never reported back")
	}
	if !strings.Contains(w.m.log.render(80, false), "tokens smaller") {
		t.Errorf("no report of what compaction saved:\n%s", w.m.log.render(80, false))
	}
}

// answered says whether a question was replied to, without waiting forever if it was not.

func TestTwoQuestionsAreAnsweredOneAtATime(t *testing.T) {
	m := newTestModel()
	first, second := make(chan agent.Decision, 1), make(chan agent.Decision, 1)
	m.Update(confirmMsg{ev: agent.Event{Kind: agent.Confirm, Text: "write src/a.go?"}, reply: first})
	m.Update(confirmMsg{ev: agent.Event{Kind: agent.Confirm, Text: "write src/b.go?"}, reply: second})

	screen := m.View()
	if !strings.Contains(screen, "write src/a.go?") {
		t.Errorf("the second question cut in front:\n%s", screen)
	}
	if !strings.Contains(screen, "1 more waiting") {
		t.Error("the screen hides that another call is waiting")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	got, ok := answered(first)
	if !ok {
		t.Fatal("the first question is still waiting: its caller is blocked")
	}
	if got != agent.Allow {
		t.Errorf("the first question got %v", got)
	}
	if !strings.Contains(m.View(), "write src/b.go?") {
		t.Error("the second question never came up")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	got, ok = answered(second)
	if !ok {
		t.Fatal("the second question is still waiting")
	}
	if got != agent.Refuse {
		t.Errorf("the second question got %v", got)
	}
}

func TestATypedEnterSendsOneLine(t *testing.T) {
	w := newWiredModel(t, "already fixed")
	w.m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix the bug")})
	w.m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(w.m.history) != 1 || w.m.history[0] != "fix the bug" {
		t.Fatalf("typed keys did not make a prompt: %v", w.m.history)
	}
}

func TestCtrlJStartsANewLineInsteadOfSending(t *testing.T) {
	for _, newline := range []tea.KeyMsg{{Type: tea.KeyCtrlJ}, {Type: tea.KeyEnter, Alt: true}} {
		w := newWiredModel(t, "ran them")
		w.m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("go test ./...")})
		w.m.Update(newline)
		if len(w.m.history) != 0 {
			t.Fatalf("%s sent the prompt", newline)
		}
		w.m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("run the ones that fail")})
		w.m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if len(w.m.history) != 1 {
			t.Fatalf("%s: nothing was sent afterwards", newline)
		}
		if !strings.Contains(w.m.history[0], "\n") || !strings.Contains(w.m.history[0], "run the ones that fail") {
			t.Errorf("%s lost the second line: %q", newline, w.m.history[0])
		}
	}
}

func TestAPasteKeepsItsOwnNewlines(t *testing.T) {
	m := newTestModel()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("func main() {\n\tprintln(1)\n}"), Paste: true})
	if got := m.input.Value(); strings.Count(got, "\n") != 2 {
		t.Errorf("the paste lost its shape: %q", got)
	}
	if len(m.history) != 0 {
		t.Error("a paste submitted the prompt")
	}
}

func TestPastingTheWordEnterDoesNotSend(t *testing.T) {
	m := newTestModel()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("enter"), Paste: true})
	if len(m.history) != 0 {
		t.Error(`pasting the word "enter" sent the prompt`)
	}
	if m.input.Value() != "enter" {
		t.Errorf("the pasted word vanished: %q", m.input.Value())
	}
}
