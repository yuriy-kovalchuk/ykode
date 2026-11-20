package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// tokens faster than the screen arrive joined: one piece per kind, in order, speeds folded
func TestWhatStreamsFasterThanFramesArrivesJoined(t *testing.T) {
	hub := NewHub()
	woken := make(chan tea.Msg, 8)
	hub.send = func(msg tea.Msg) { woken <- msg }

	hub.Emit(agent.Event{Kind: agent.Thinking, Text: "let me "})
	hub.Emit(agent.Event{Kind: agent.Thinking, Text: "look"})
	hub.Emit(agent.Event{Kind: agent.Answer, Text: "the "})
	hub.Emit(agent.Event{Kind: agent.Progress, Stats: agent.Stats{PrefillTokSec: 400}})
	hub.Emit(agent.Event{Kind: agent.Answer, Text: "answer"})
	hub.Emit(agent.Event{Kind: agent.Progress, Stats: agent.Stats{DecodeTokSec: 30}})
	hub.Emit(agent.Event{Kind: agent.ToolDraft, Call: 0, Tool: "write_file", Text: `{"path":`})
	hub.Emit(agent.Event{Kind: agent.ToolDraft, Call: 0, Tool: "write_file", Text: `"a.go"`})
	hub.Emit(agent.Event{Kind: agent.ToolDraft, Call: 1, Tool: "read_file", Text: `{}`})

	if _, ok := (<-woken).(wakeMsg); !ok {
		t.Fatal("the screen was never told to look")
	}
	var got []string
	for _, msg := range hub.take() {
		ev := msg.(eventMsg).ev
		got = append(got, fmt.Sprintf("%s %q %.0f/%.0f", ev.Kind, ev.Text, ev.Stats.PrefillTokSec, ev.Stats.DecodeTokSec))
	}
	want := []string{
		`thinking "let me look" 0/0`, `answer "the answer" 0/0`, `progress "" 400/30`,
		`tool_draft "{\"path\":\"a.go\"" 0/0`, `tool_draft "{}" 0/0`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the queue held:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	select {
	case <-woken:
		t.Error("the screen was told to look more than once for one queue")
	default:
	}
}

// a run never waits for the screen: what it reports is queued even while nobody is taking it
func TestARunDoesNotWaitForTheScreen(t *testing.T) {
	hub := NewHub()
	hub.send = func(tea.Msg) { select {} } // a screen that never comes back
	for i := 0; i < 100; i++ {
		hub.Emit(agent.Event{Kind: agent.Answer, Text: "x"})
	}
	if queued := hub.take(); len(queued) != 1 || len(queued[0].(eventMsg).ev.Text) != 100 {
		t.Errorf("the hub queued %d messages for 100 tokens", len(queued))
	}
}

// what the hub holds reaches the session when the screen is woken, as if each had come alone
func TestBeingWokenDrawsWhatTheRunReported(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.hub.send = func(tea.Msg) {}
	m.hub.Emit(agent.Event{Kind: agent.Answer, Text: "the answer, "})
	m.hub.Emit(agent.Event{Kind: agent.Answer, Text: "in two pieces"})
	m.hub.Emit(agent.Event{Kind: agent.Notice, Text: "and a notice"})
	m.Update(wakeMsg{})
	out := stripCodes(m.view.view())
	if !strings.Contains(out, "the answer, in two pieces") || !strings.Contains(out, "and a notice") {
		t.Errorf("what the run reported is not on the screen:\n%s", out)
	}
}

// what streams draws at frame pace: the first piece at once, the rest with the frame due
func TestWhatStreamsIsDrawnOncePerFrame(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.drawn = time.Time{} // nothing was drawn for a while, as when a reply begins
	_, cmd := m.Update(eventMsg{ev: agent.Event{Kind: agent.Answer, Text: "the first piece"}})
	if !strings.Contains(stripCodes(m.view.view()), "the first piece") || cmd != nil {
		t.Fatal("the first piece of a reply waited for a frame")
	}
	_, cmd = m.Update(eventMsg{ev: agent.Event{Kind: agent.Answer, Text: " and the second"}})
	if strings.Contains(stripCodes(m.view.view()), "and the second") {
		t.Error("a piece inside the same frame was drawn at the token's own rate")
	}
	if cmd == nil {
		t.Fatal("no frame was asked for to draw what waits")
	}
	if _, cmd = m.Update(eventMsg{ev: agent.Event{Kind: agent.Answer, Text: " and a third"}}); cmd != nil {
		t.Error("a second frame was asked for while one was already due")
	}
	m.Update(frameMsg{})
	if !strings.Contains(stripCodes(m.view.view()), "and the second and a third") {
		t.Error("the frame never drew what was waiting")
	}
}

// aStreamedAnswer is an answer as a model writes one: prose, lists and code, a few times over
func aStreamedAnswer(sections int) string {
	var b strings.Builder
	for i := 0; i < sections; i++ {
		fmt.Fprintf(&b, "## Section %d\n\nThe file has **three** parts and a `main` function, and %s\n\n- the record, its rows\n- the text rules\n- the wrapping\n\n", i, strings.Repeat("some prose to fill the row. ", 12))
		b.WriteString("```go\n")
		for j := 0; j < 30; j++ {
			fmt.Fprintf(&b, "func handler%d(w http.ResponseWriter, r *http.Request) error {\n\tif err := check(r, \"value %d\"); err != nil { // why\n\t\treturn fmt.Errorf(\"handler: %%w\", err)\n\t}\n\treturn nil\n}\n", j, j)
		}
		b.WriteString("```\n\n")
	}
	return b.String()
}

// BenchmarkAnAnswerStreamed is a whole answer arriving; per op is the screen's share of it
func BenchmarkAnAnswerStreamed(b *testing.B) {
	for _, sections := range []int{1, 4} {
		answer := aStreamedAnswer(sections)
		b.Run(fmt.Sprintf("%dKB", len(answer)/1024), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				m := newTestModel()
				m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
				m.running = true
				for at, tokens := 0, 0; at < len(answer); at, tokens = at+4, tokens+1 {
					m.handle(agent.Event{Kind: agent.Answer, Text: answer[at:min(at+4, len(answer))]})
					if tokens%20 == 0 {
						m.refresh()
						_ = m.render()
					}
				}
			}
		})
	}
}

// BenchmarkCodeColoured is what a grammar costs for a block of a few kilobytes, read whole.
func BenchmarkCodeColoured(b *testing.B) {
	answer := aStreamedAnswer(1)
	code := answer[strings.Index(answer, "```go\n")+6 : strings.LastIndex(answer, "```")]
	b.SetBytes(int64(len(code)))
	for b.Loop() {
		coloured(lexerFor("go"), code)
	}
}

// thinking still arriving is wrapped a paragraph at a time, and reads exactly as it would wrapped whole
func TestThinkingWrappedAsItArrivesMatchesItWrappedWhole(t *testing.T) {
	thought := "first, what the file holds and why it matters here.\n\nthen the rows, " +
		strings.Repeat("which run on for a while ", 8) + "\nand a last line"
	plain := func(row string) string { return row }
	var arriving flow
	for upto := 1; upto <= len(thought); upto++ {
		got := strings.Join(arriving.wrap(thought[:upto], 30, plain), "\n")
		if want := strings.Join(wrap(thought[:upto], 30), "\n"); got != want {
			t.Fatalf("after %d bytes:\narriving %q\nwhole    %q", upto, got, want)
		}
	}
}

// a reply ends once per round-trip: the calls it was writing go, and nothing above them is drawn again
func TestDroppingTheDraftsKeepsWhatWasDrawnAboveThem(t *testing.T) {
	var log transcript
	log.user("count the files", time.Now())
	log.answer("looking")
	log.draft(0, "write_file", `{"path":"a.go"`)
	log.rows(80)
	log.dropDrafts()
	if log.stale != 2 || len(log.entries) != 2 {
		t.Errorf("drawing starts again at entry %d of %d, want only from the draft that went", log.stale, len(log.entries))
	}
	log.rows(80)
	log.dropDrafts() // a reply that wrote no call changes nothing
	if log.hasPending() {
		t.Error("a reply with no drafts sent the session to be drawn again")
	}
}

// a call that takes no arguments is still waiting until it answers
func TestACallWithNoArgumentsWaitsForItsAnswer(t *testing.T) {
	var log transcript
	log.toolStart("plan", "")
	if out := log.render(80); !strings.Contains(out, "▸") || strings.Contains(out, "✓") {
		t.Errorf("a call that has not answered reads as done:\n%s", out)
	}
	log.toolEnd("plan", "", "plan 1/2")
	if out := log.render(80); strings.Count(out, "plan") < 1 || !strings.Contains(out, "✓") || strings.Contains(out, "▸") {
		t.Errorf("the answer did not land on the call's own row:\n%s", out)
	}
}

// a character outside the basic plane is two escapes in JSON, and one character on the screen
func TestAnEscapedEmojiIsOneCharacter(t *testing.T) {
	if got := unescape(`ok 😀 done`); got != "ok 😀 done" {
		t.Errorf("got %q", got)
	}
	if got := unescape(`half \ud83d`); got != "half " {
		t.Errorf("half an escape drew %q", got)
	}
}

// the note beside a suggested command is counted in columns: colour takes bytes, no room
func TestASuggestionKeepsItsNoteWhenColoured(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	m.suggest = suggestions("/us")
	rows := m.suggestRows(m.height) // room to spare: this test is about the columns, not the rows
	if len(rows) != 1 || lipgloss.Width(rows[0]) < 50 {
		t.Errorf("the note was cut short of the screen: %q", stripCodes(strings.Join(rows, "\n")))
	}
}

// a run waiting on a question is let go when the screen leaves: refused, not left holding it
func TestAQuestionIsRefusedWhenTheScreenLeaves(t *testing.T) {
	hub := NewHub()
	hub.wire(func(tea.Msg) {}) // a screen that takes the question and never answers it
	answered := make(chan agent.Decision, 1)
	go func() { answered <- hub.Emit(agent.Event{Kind: agent.Confirm, Text: "write a.go?"}) }()
	hub.close()
	hub.close() // leaving twice is leaving
	select {
	case got := <-answered:
		if got != agent.Refuse {
			t.Errorf("the run was answered %v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run is still waiting for a screen that is gone")
	}
}
