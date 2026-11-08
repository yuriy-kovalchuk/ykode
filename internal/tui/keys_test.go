package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

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

// ctrl+p and ctrl+n walk the prompts sent this session, and what was typed but not sent waits at
// the end of the walk instead of being overwritten
func TestCtrlPWalksOlderPromptsAndKeepsWhatWasTyped(t *testing.T) {
	m := newTestModel()
	m.history = []string{"first question", "second question"}
	m.histIdx = len(m.history)

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.input.Value() != "second question" {
		t.Errorf("ctrl+p gave %q", m.input.Value())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if m.input.Value() != "second question" {
		t.Errorf("ctrl+n gave %q", m.input.Value())
	}

	m.histIdx = len(m.history) // as it sits after a prompt was sent
	m.input.SetValue("typing")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if m.input.Value() != "typing" {
		t.Errorf("walking back past the end lost the line being typed: %q", m.input.Value())
	}
}

// a trackpad scroll reaches a terminal that is not holding the mouse as arrow keys: it moves the
// session, and never writes a prompt into the box
func TestAScrollMovesTheSessionNotThePrompt(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	for i := 0; i < 40; i++ {
		m.log.notice(fmt.Sprintf("row %d of a long session", i))
	}
	m.refresh()
	if !m.view.AtBottom() {
		t.Fatal("the session did not start at its end")
	}

	for i := 0; i < 4; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	if m.view.AtBottom() {
		t.Error("the arrows did not move the session")
	}
	if m.input.Value() != "" {
		t.Errorf("a scroll wrote into the box: %q", m.input.Value())
	}
	if m.histIdx != len(m.history) {
		t.Errorf("a scroll walked the prompt history to %d", m.histIdx)
	}
}

// with more than one line in the box the arrows move the caret through that text, not the session
func TestTheArrowsMoveTheCaretInAMultiLinePrompt(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	for i := 0; i < 40; i++ {
		m.log.notice(fmt.Sprintf("row %d of a long session", i))
	}
	m.refresh()
	m.input.SetValue("one\ntwo")
	m.refresh()

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.input.Line() != 1 {
		t.Errorf("the caret is on line %d, want it moved down through the prompt", m.input.Line())
	}
	if m.input.Value() != "one\ntwo" {
		t.Errorf("the box changed: %q", m.input.Value())
	}
}

// growing the box while reading the end of the session keeps the end on screen
func TestGrowingTheBoxKeepsTheEndOfTheSession(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	for i := 0; i < 40; i++ {
		m.log.notice(fmt.Sprintf("row %d of a long session", i))
	}
	m.refresh()

	m.input.SetValue("one\ntwo")
	m.refresh()
	if !m.view.AtBottom() {
		t.Errorf("the session lifted off its end as the box grew: scrolled to %.0f%%", m.view.ScrollPercent()*100)
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

// the list of keys has to open while a prompt is half typed, which ? cannot do
func TestCtrlSlashShowsTheKeysOverATypedPrompt(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("count the lines")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlUnderscore})
	if !m.showKeys {
		t.Fatal("ctrl+? over typed text did not open the list")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlUnderscore})
	if m.showKeys {
		t.Fatal("ctrl+? again did not hide the list")
	}
}
