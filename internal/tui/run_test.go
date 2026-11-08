package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// an ask typed into a running turn goes to that run, which takes it at the next round-trip, and
// stays on the screen until it does
func TestAnAskTypedIntoARunningTurnWaitsAboveThePrompt(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	m.running = true
	m.input.SetValue("also check the tests")

	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("an ask handed to the run should not start another")
	}
	if got := m.agent.Waiting(); len(got) != 1 || got[0] != "also check the tests" {
		t.Fatalf("the run was not handed the ask: %v", got)
	}
	out := stripCodes(m.View())
	if !strings.Contains(out, "also check the tests") || !strings.Contains(out, "waits for the next round-trip") {
		t.Errorf("the waiting ask is not kept above the prompt:\n%s", out)
	}
}

// an ask the run took is drawn where the model reads it, not only as a waiting row
func TestAnAskTheRunTookIsDrawnWhereTheModelReadsIt(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	m.running = true
	m.agent.Steer("and the tests too")

	m.handle(agent.Event{Kind: agent.Steered, Text: "and the tests too", Turn: 2})
	block := m.log.render(80, false)
	if !strings.Contains(block, "and the tests too") {
		t.Errorf("the ask is not in the session:\n%s", block)
	}
}

// an ask still waiting when the run ended starts a turn of its own
func TestAnAskLeftOverWhenTheRunEndedStartsItsOwnTurn(t *testing.T) {
	w := newWiredModel(t, "ran them")
	w.m.width, w.m.height = 80, 24
	w.m.running = true
	w.m.agent.Steer("then run the tests")

	if _, cmd := w.m.Update(runDoneMsg{}); cmd == nil {
		t.Fatal("a waiting ask should start once the run is over")
	}
	if !w.m.running {
		t.Error("the ask did not start a run")
	}
	if !strings.Contains(w.m.log.render(80, false), "then run the tests") {
		t.Error("the ask is not on the screen as a prompt of its own")
	}
	if got := w.m.agent.Waiting(); len(got) != 0 {
		t.Errorf("the ask is still waiting after it started: %v", got)
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

func TestCompactingDoesNotStopTheScreen(t *testing.T) {
	w := newWiredModel(t, "goal: fix the timeout\nfiles: src/a.go\nnext: run the tests")
	w.m.input.SetValue("/compact")
	if _, cmd := w.m.submit(); quits(cmd) {
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

// a turn's end reports both speeds, and a run is timed from the moment it starts
func TestTheScreenTimesARunAndReportsItsSpeeds(t *testing.T) {
	m := newTestModel()
	m.handle(agent.Event{Kind: agent.TurnEnd, Stats: agent.Stats{PrefillTokSec: 480, DecodeTokSec: 31}})
	if s := m.status(); s.prefill != 480 || s.decode != 31 {
		t.Errorf("status = %+v, want both speeds from the turn", s)
	}
	m.begin()
	m.started = m.started.Add(-90 * time.Second)
	if s := m.status(); !s.running || s.elapsed < 90*time.Second {
		t.Errorf("a running prompt is not timed: %+v", s)
	}
	m.Update(runDoneMsg{})
	if s := m.status(); s.running || s.took < 90*time.Second {
		t.Errorf("a finished prompt does not say how long it took: %+v", s)
	}
}

// the speeds update while the reply streams, and a token that reports none yet keeps the last speed
func TestTheSpeedsUpdateWhileTheReplyStreams(t *testing.T) {
	m := newTestModel()
	m.handle(agent.Event{Kind: agent.Progress, Stats: agent.Stats{PrefillTokSec: 430, DecodeTokSec: 0}})
	m.handle(agent.Event{Kind: agent.Progress, Stats: agent.Stats{DecodeTokSec: 41}})
	m.handle(agent.Event{Kind: agent.Progress, Stats: agent.Stats{DecodeTokSec: 0}})
	if s := m.status(); s.prefill != 430 || s.decode != 41 {
		t.Errorf("status = prefill %.0f decode %.0f, want 430 and 41", s.prefill, s.decode)
	}
}

// what failed last turn is not a notice the screen keeps reading: the next prompt clears the row
func TestANewRunClearsTheErrorRow(t *testing.T) {
	m := newTestModel()
	m.err = "the server hung up mid-turn"
	if !strings.Contains(m.bottomArea(), "the server hung up") {
		t.Fatal("a failed run left no row to clear")
	}
	m.begin()
	if strings.Contains(m.bottomArea(), "the server hung up") {
		t.Error("the old failure is still on screen while a new turn runs")
	}
}

// a call being written is drawn once per spin, not once per token: the stream that feeds it would
// otherwise wait for the screen every time a fragment arrives
func TestACallBeingWrittenWaitsForTheTickToBeDrawn(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.running = true
	m.handle(agent.Event{Kind: agent.ToolDraft, Call: 0, Tool: "write_file", Text: `{"path":"a.go","content":"the first line`})
	if strings.Contains(stripCodes(m.view.View()), "the first line") {
		t.Error("the call being written was drawn at the token's own rate")
	}
	m.Update(spinner.TickMsg{})
	if !strings.Contains(stripCodes(m.view.View()), "the first line") {
		t.Error("the next tick never drew the call being written")
	}
}

// what the server reports stays on the screen while a call is still being written
func TestTheSpeedsStayLiveWhileACallIsBeingWritten(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m.running = true
	m.handle(agent.Event{Kind: agent.ToolDraft, Call: 0, Tool: "write_file", Text: `{"path":"a.go","content":"x`})
	m.handle(agent.Event{Kind: agent.Progress, Stats: agent.Stats{PrefillTokSec: 512.4, DecodeTokSec: 71.3}})
	line := stripCodes(m.View())
	if !strings.Contains(line, "↑ 512 tok/s") || !strings.Contains(line, "↓ 71 tok/s") {
		t.Errorf("a queued transcript draw hid the speeds: %s", line)
	}
}

// one token of a long write: a fragment joins the record, the screen follows on the tick
func BenchmarkWriteAFileToken(b *testing.B) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.running = true
	m.handle(agent.Event{Kind: agent.ToolDraft, Call: 0, Tool: "write_file", Text: `{"path":"src/big.go","content":"`})
	tick := spinner.TickMsg{}
	for i := 0; i < b.N; i++ {
		m.handle(agent.Event{Kind: agent.ToolDraft, Call: 0, Tool: "write_file", Text: "a line of source, byte after byte\\n"})
		if i%10 == 0 {
			m.Update(tick)
		}
	}
}
