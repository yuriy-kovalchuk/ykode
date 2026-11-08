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

// waitFor waits for the screen to show want, until the test's own -timeout: a busy machine is not a failure.
func waitFor(t *testing.T, screen *lockedBuffer, want string) {
	t.Helper()
	deadline, ok := t.Deadline()
	if !ok {
		deadline = time.Now().Add(time.Minute)
	}
	deadline = deadline.Add(-2 * time.Second) // fail here with the screen, not in go test's panic
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

func writeKeys(t *testing.T, keys *os.File, burst string) {
	t.Helper()
	if _, err := keys.WriteString(burst); err != nil {
		t.Fatalf("sending %q: %v", burst, err)
	}
}

// lockedBuffer lets the test read what the screen was given while it is still being written.

type wired struct {
	m    *model
	msgs chan tea.Msg
}

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

func answered(ch <-chan agent.Decision) (agent.Decision, bool) {
	select {
	case d := <-ch:
		return d, true
	default:
		return 0, false
	}
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
	go func() { done <- run(context.Background(), a, hub, false, tea.WithInput(in), tea.WithOutput(screen)) }()

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
