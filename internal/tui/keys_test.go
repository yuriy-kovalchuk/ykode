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

	if !strings.Contains(m.View(), "y yes · a always · n no") {
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

// ctrl+p and ctrl+n walk the sent prompts, with the unsent one waiting at the end of the walk
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

// a trackpad scroll arrives as arrow keys: it moves the session, never writing into the box
func TestAScrollMovesTheSessionNotThePrompt(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	for i := 0; i < 40; i++ {
		m.log.notice(fmt.Sprintf("row %d of a long session", i))
	}
	m.refresh()
	if !m.view.atBottom() {
		t.Fatal("the session did not start at its end")
	}

	for i := 0; i < 4; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	if m.view.atBottom() {
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
	if !m.view.atBottom() {
		t.Errorf("the session lifted off its end as the box grew: scrolled to %.0f%%", m.view.percent()*100)
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

// the box is a readline box: ctrl+a and ctrl+e move within the caret's line, not the prompt
func TestCtrlAAndCtrlEMoveTheCaretWithinTheirLine(t *testing.T) {
	m := inputModel(40)
	typeText(m, "one")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	typeText(m, "two three")

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	typeText(m, "X")
	if got := m.input.Value(); got != "one\nXtwo three" {
		t.Fatalf("ctrl+a put the caret somewhere else: %q", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	typeText(m, "Y")
	if got := m.input.Value(); got != "one\nXtwo threeY" {
		t.Errorf("ctrl+e put the caret somewhere else: %q", got)
	}
}

// ctrl+k kills the line ahead of the caret, ctrl+w the word before it
func TestCtrlKAndCtrlWKillWhatTheyPointAt(t *testing.T) {
	m := inputModel(40)
	typeText(m, "one two three")

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	typeText(m, "!")
	if got := m.input.Value(); got != "one two !" {
		t.Errorf("ctrl+w left %q, want the word before the caret gone", got)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if got := m.input.Value(); got != "" {
		t.Errorf("ctrl+k left %q, want the line ahead of the caret killed", got)
	}
}

// ctrl+u stays the transcript's: killing a line is ctrl+k's work, and one key does one job
func TestCtrlUScrollsWithTextInTheBox(t *testing.T) {
	m := longSession(t)
	typeText(m, "beta gamma")
	before := m.view.at

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.view.at != before-m.pageRows(true) {
		t.Errorf("ctrl+u moved the transcript from row %d to %d, want half a page up", before, m.view.at)
	}
	if got := m.input.Value(); got != "beta gamma" {
		t.Errorf("ctrl+u edited the prompt, leaving %q", got)
	}
}
