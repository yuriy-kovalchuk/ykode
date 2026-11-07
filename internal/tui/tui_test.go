package tui

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

func TestThinkingArrivesInPiecesAndReadsAsOneBlock(t *testing.T) {
	var log transcript
	log.thinking("the bug ")
	log.thinking("is in main.go")
	log.answer("fix it here")

	if len(log.entries) != 2 {
		t.Fatalf("want one thinking block and one answer, got %d", len(log.entries))
	}
	if log.entries[0].text != "the bug is in main.go" {
		t.Errorf("the pieces did not join: %q", log.entries[0].text)
	}
	out := log.render(80, false)
	if !strings.Contains(out, "the bug is in main.go") || !strings.Contains(out, "fix it here") {
		t.Errorf("rendered transcript is missing what was said:\n%s", out)
	}
}

func TestAParallelReplyLandsOnItsOwnCall(t *testing.T) {
	var log transcript
	log.toolStart("read_file", `{"path":"a.go"}`)
	log.toolStart("read_file", `{"path":"b.go"}`)
	// the first call answers last: the reply has to find its own row, not the newest one
	log.toolEnd("read_file", `{"path":"a.go"}`, "a has 3 funcs")

	if log.entries[0].result != "a has 3 funcs" {
		t.Errorf("a.go's reply landed on another row: %q", log.entries[0].result)
	}
	if log.entries[1].result != "" {
		t.Errorf("b.go was answered with a.go's result: %q", log.entries[1].result)
	}
	out := log.render(80, false)
	if !strings.Contains(out, "working…") {
		t.Errorf("a call still running should say so:\n%s", out)
	}
}

func TestToolRowsShowMoreWhenOpened(t *testing.T) {
	var log transcript
	log.toolStart("edit_file", `{"path":"src/a.go","old_string":"one very long argument list that does not fit a line"}`)
	log.toolEnd("edit_file", `{"path":"src/a.go","old_string":"one very long argument list that does not fit a line"}`, "1 edit applied")

	long := `{"path":"src/a.go","old_string":"one very long argument list that does not fit a line"}`
	collapsed := log.render(80, false)
	if strings.Contains(collapsed, "does not fit a line") {
		t.Errorf("a closed row ran past the result:\n%s", collapsed)
	}
	opened := log.render(80, true)
	if !strings.Contains(opened, `a line"}`) {
		t.Errorf("opening the row did not show what it was asked to do:\n%s", opened)
	}
	for _, line := range strings.Split(collapsed, "\n") {
		if len(stripCodes(line)) > 80 {
			t.Errorf("a row is wider than the screen: %q", stripCodes(line))
		}
	}
	if log.render(40, false) == "" {
		t.Error("a narrow screen rendered nothing")
	}
	if !strings.Contains(log.render(40, false), long[:12]) {
		t.Error("even a narrow row lost the path")
	}
}

func TestAFailedCallIsMarkedAsOne(t *testing.T) {
	var log transcript
	log.failed("write_file", "user refused")
	out := log.render(80, false)
	if !strings.Contains(out, "✗") || !strings.Contains(out, "user refused") {
		t.Errorf("a refused call is not obvious:\n%s", out)
	}
}

func TestLongThinkingWrapsInsteadOfOverflowing(t *testing.T) {
	var log transcript
	log.thinking(strings.Repeat("word ", 60))
	lines := strings.Split(log.render(40, false), "\n")
	if len(lines) < 5 {
		t.Fatalf("a long block stayed on too few lines: %d", len(lines))
	}
	for _, line := range lines {
		if len(strings.TrimSpace(stripCodes(line))) > 40 {
			t.Errorf("a line is wider than the screen: %q", stripCodes(line))
		}
	}
}

func TestTheStatusLineSaysWhatTheSessionCosts(t *testing.T) {
	usage := agent.Usage{Turns: 3, PromptTok: 1000, CachedTok: 900, EstTok: 12000}
	line := statusLine("Qwen3.6-35B-A3B", usage, 131072, 41.6, true, 2)
	for _, want := range []string{"Qwen3.6-35B-A3B", "12.0k/131k", "cache 90%", "42 tok/s", "2 queued"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q missing from: %s", want, line)
		}
	}
}

func TestTheStatusLineKeepsQuietNumbersQuiet(t *testing.T) {
	line := statusLine("m", agent.Usage{}, 0, 0, false, 0)
	if strings.Contains(line, "tok/s") || strings.Contains(line, "cache") || strings.Contains(line, "ctx /") {
		t.Errorf("invented numbers before the server gave any: %s", line)
	}
	if !strings.Contains(line, "m") {
		t.Errorf("the model vanished: %s", line)
	}
}

// stripCodes removes the colour escapes so a test can measure what the terminal shows.
func stripCodes(s string) string {
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], "m")
		if j < 0 {
			return s
		}
		s = s[:i] + s[i+j+1:]
	}
}

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

func TestTheHubRefusesWhileNothingIsOnScreen(t *testing.T) {
	hub := NewHub()
	if got := hub.Emit(agent.Event{Kind: agent.Confirm, Text: "write src/a.go?"}); got != agent.Refuse {
		t.Errorf("with no screen to answer, the hub said %v", got)
	}
}

func TestTheHubWaitsForWhatTheScreenAnswers(t *testing.T) {
	hub := NewHub()
	var got []agent.Event
	var answer agent.Decision = agent.AllowAlways
	hub.send = func(msg tea.Msg) {
		switch m := msg.(type) {
		case eventMsg:
			got = append(got, m.ev)
		case confirmMsg:
			m.reply <- answer
		}
	}

	hub.Emit(agent.Event{Kind: agent.ToolEnd, Tool: "read_file", Text: "3 lines"})
	if len(got) != 1 || got[0].Tool != "read_file" {
		t.Errorf("the run never reached the screen: %v", got)
	}
	if verdict := hub.Emit(agent.Event{Kind: agent.Confirm, Text: "write src/a.go?"}); verdict != answer {
		t.Errorf("the run got %v back, not what the screen said (%v)", verdict, answer)
	}
}

// stubAnswers answers each request with its own text, the way a server would.
type stubAnswers struct {
	mu      sync.Mutex
	answers []string
	seen    int
}

func (s *stubAnswers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	s.mu.Lock()
	i := s.seen
	s.seen++
	s.mu.Unlock()
	if i >= len(s.answers) {
		i = len(s.answers) - 1
	}
	fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", s.answers[i])
}

func writeKeys(t *testing.T, keys *os.File, burst string) {
	t.Helper()
	if _, err := keys.WriteString(burst); err != nil {
		t.Fatalf("sending %q: %v", burst, err)
	}
}

// lockedBuffer lets the test read what the screen was given while it is still being written.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor says whether the screen showed want before the deadline.
func waitFor(t *testing.T, screen *lockedBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(stripCodes(screen.String()), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the screen never showed %q:\n%s", want, stripCodes(screen.String()))
}

// TestTwoPromptsInARowBothRun drives the real wiring: keys in, a turn on its own goroutine,
// the screen redrawn from what that turn reported.
func TestTwoPromptsInARowBothRun(t *testing.T) {
	server := httptest.NewServer(&stubAnswers{answers: []string{"answer one", "answer two"}})
	defer server.Close()

	hub := NewHub()
	a := agent.New(llm.NewClient(server.URL+"/v1", ""), tools.NewRegistry(), "test-model",
		agent.Options{MaxTurns: 2, MaxTokens: 64, CtxSize: 8000}, hub)
	// the program reads keys from a file descriptor, so an os pipe paces it the way a
	// terminal does: one burst per step of the test
	in, keys, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer keys.Close()
	defer in.Close()
	screen := &lockedBuffer{}

	done := make(chan error, 1)
	go func() { done <- run(context.Background(), a, hub, tea.WithInput(in), tea.WithOutput(screen)) }()

	writeKeys(t, keys, "first\r")
	waitFor(t, screen, "answer one")
	writeKeys(t, keys, "second\r")
	waitFor(t, screen, "answer two")

	writeKeys(t, keys, "/exit\r")
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the session ended with an error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the session would not exit")
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
type wired struct {
	m    *model
	msgs chan tea.Msg
}

func newWiredModel(t *testing.T, answers ...string) *wired {
	t.Helper()
	server := httptest.NewServer(&stubAnswers{answers: answers})
	t.Cleanup(server.Close)
	hub := NewHub()
	a := agent.New(llm.NewClient(server.URL+"/v1", ""), tools.NewRegistry(), "test-model",
		agent.Options{MaxTurns: 2, MaxTokens: 64, CtxSize: 8000}, hub)
	m := newModel(context.Background(), a, hub)
	msgs := make(chan tea.Msg, 32)
	hub.send = func(msg tea.Msg) { msgs <- msg }
	return &wired{m: m, msgs: msgs}
}

// feed runs whatever the session reported, the way a running program would.
func (w *wired) feed() {
	for {
		select {
		case msg := <-w.msgs:
			w.m.Update(msg)
		default:
			return
		}
	}
}

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
func answered(ch <-chan agent.Decision) (agent.Decision, bool) {
	select {
	case d := <-ch:
		return d, true
	default:
		return 0, false
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
