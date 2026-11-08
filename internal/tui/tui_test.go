package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"io"
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
	m := newModel(context.Background(), a, hub, config.Config{})
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
	go func() {
		done <- run(context.Background(), a, hub, config.Config{}, tea.WithInput(in), tea.WithOutput(screen))
	}()

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

// stubPaused holds its first reply open until the test lets it go, so the test can type into a
// turn that is provably still running.
type stubPaused struct {
	mu       sync.Mutex
	release  chan struct{}
	requests []map[string]any
}

func (s *stubPaused) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		panic(fmt.Sprintf("the server got an unmarshalable request: %v", err))
	}
	s.mu.Lock()
	first := len(s.requests) == 0
	s.requests = append(s.requests, req)
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	if first {
		<-s.release
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1",`+
			`"function":{"name":"find_files","arguments":"{}"}}]}}],"finish_reason":"tool_calls"}`+"\n\ndata: [DONE]\n\n")
		return
	}
	fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"answered while you waited"},`+
		`"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
}

func (s *stubPaused) asked() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *stubPaused) carries(text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, req := range s.requests[1:] { // the ask cannot be in the request that was already open
		for _, raw := range req["messages"].([]any) {
			if m := raw.(map[string]any); m["role"] == "user" && strings.Contains(fmt.Sprint(m["content"]), text) {
				return true
			}
		}
	}
	return false
}

// an ask typed into a turn that is still running reaches the model inside that turn, rather than
// waiting for the whole turn to end
func TestAnAskTypedMidRunReachesTheModelInThatRun(t *testing.T) {
	stub := &stubPaused{release: make(chan struct{})}
	server := httptest.NewServer(stub)
	defer server.Close()

	hub := NewHub()
	a := agent.New(llm.NewClient(server.URL+"/v1", ""), tools.NewRegistry(), "test-model",
		agent.Options{MaxTurns: 3, MaxTokens: 64, CtxSize: 8000}, hub)
	in, keys, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer keys.Close()
	defer in.Close()
	screen := &lockedBuffer{}

	done := make(chan error, 1)
	go func() {
		done <- run(context.Background(), a, hub, config.Config{}, tea.WithInput(in), tea.WithOutput(screen))
	}()

	writeKeys(t, keys, "count the files\r")
	for stub.asked() == 0 { // the turn is open: its first reply has not been sent yet
		time.Sleep(10 * time.Millisecond)
	}
	writeKeys(t, keys, "and the tests too\r")
	close(stub.release)

	waitFor(t, screen, "answered while you waited")
	if !stub.carries("and the tests too") {
		t.Errorf("no later request carried the ask typed mid-turn:\n%s", stripCodes(screen.String()))
	}
	if !strings.Contains(stripCodes(screen.String()), "and the tests too") {
		t.Errorf("the ask typed mid-turn was never drawn:\n%s", stripCodes(screen.String()))
	}

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
