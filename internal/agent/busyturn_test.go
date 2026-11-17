package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
)

// heldServer answers only when told to, so a turn can be caught in flight
type heldServer struct {
	release chan struct{}
	once    sync.Once
}

func (h *heldServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	if flusher, ok := w.(http.Flusher); ok {
		// one byte, so the client is genuinely inside the reply rather than still dialling
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"wor\"}}]}\n\n")
		flusher.Flush()
	}
	<-h.release
	fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"k\"},\"finish_reason\":\"stop\"}]}\n\n")
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (h *heldServer) done() { h.once.Do(func() { close(h.release) }) }

// a between-turns change refuses while a turn runs, and says so in words the user can act on
func TestAChangeBetweenTurnsRefusesWhileOneRuns(t *testing.T) {
	held := &heldServer{release: make(chan struct{})}
	server := httptest.NewServer(held)
	defer func() { held.done(); server.Close() }()

	a := New(llm.NewClient(server.URL+"/v1", ""), nil, "test-model", Options{}, &captureSink{})
	if a.turnBusy() {
		t.Fatal("a fresh agent says a turn is running")
	}

	started := make(chan struct{})
	askErr := make(chan error, 1)
	go func() {
		close(started)
		askErr <- a.Ask(context.Background(), "work on it")
	}()
	<-started
	if !waitUntil(func() bool { return a.turnBusy() }, time.Second) {
		held.done()
		t.Fatal("the turn never marked the run busy")
	}

	// every one of these rewrites something the round-trip in flight is reading
	if a.SetEndpoint(llm.NewClient(server.URL+"/v1", ""), llm.ModelInfo{ID: "other-model"}, 4096) {
		t.Error("switching server was accepted while a turn ran")
	}
	if a.SetModel("another-model") {
		t.Error("switching model was accepted while a turn ran")
	}
	if a.SetThinkingLevel("off") {
		t.Error("changing the thinking level was accepted while a turn ran")
	}
	if a.Tune(func(o *Options) { o.MaxTurns = 1 }) {
		t.Error("changing an option was accepted while a turn ran")
	}
	if a.Model() != "test-model" {
		t.Errorf("the model moved anyway: %q", a.Model())
	}
	if a.ThinkingLevel() == llm.ThinkingOff {
		t.Error("the thinking level moved anyway")
	}
	if err := a.ContinueFrom(t.TempDir(), "s20260101-120000.000"); err == nil ||
		!strings.Contains(err.Error(), "turn is running") {
		t.Errorf("switching conversation mid-turn: %v", err)
	}

	held.done()
	if err := <-askErr; err != nil {
		t.Errorf("the turn failed: %v", err)
	}
	if a.turnBusy() {
		t.Error("the run is still marked busy after the turn ended")
	}

	// and the same calls are simply accepted between turns
	if !a.SetModel("a-model-now") {
		t.Error("switching model was refused between turns")
	}
	if !a.Tune(func(o *Options) { o.MaxTurns = 3 }) {
		t.Error("changing an option was refused between turns")
	}
}

// a fold inside a turn must not steal the busy flag from the turn that owns the run
func TestAFoldOwnsTheRunOnlyWhenItStartedTheRun(t *testing.T) {
	stub := &stubServer{scripts: []string{answerWithTimings("a recap of the older turns", 10, 5, 5)}}
	a, done := newAgent(t, stub, Options{CtxSize: 8000}, &captureSink{})
	defer done()

	a.Compact(context.Background()) // alone: it claims and releases
	if a.turnBusy() {
		t.Error("a fold left the run marked busy")
	}

	// inside a turn: the flag is still the turn's afterwards, so the turn can release it
	a.turnRunning.Store(true)
	a.Compact(context.Background())
	if !a.turnBusy() {
		t.Error("a fold called from inside a turn released a run it did not own")
	}
	a.turnRunning.Store(false)
}

// a second turn cannot start over a running one: both would write the same conversation
func TestATurnCannotBeStartedOverOneAlreadyRunning(t *testing.T) {
	held := &heldServer{release: make(chan struct{})}
	server := httptest.NewServer(held)
	defer func() { held.done(); server.Close() }()

	a := New(llm.NewClient(server.URL+"/v1", ""), nil, "test-model", Options{}, &captureSink{})
	first := make(chan error, 1)
	go func() { first <- a.Ask(context.Background(), "the first ask") }()
	if !waitUntil(func() bool { return a.turnBusy() }, time.Second) {
		held.done()
		t.Fatal("the first turn never marked the run busy")
	}

	second := a.Ask(context.Background(), "the second ask")
	if second == nil || !strings.Contains(second.Error(), "already running") {
		t.Errorf("a second turn was accepted alongside the first: %v", second)
	}
	held.done()
	if err := <-first; err != nil {
		t.Errorf("the first turn failed: %v", err)
	}
}

// A saved conversation cannot be swapped in under a request that is being built from the old one.
func TestSwitchingConversationMidTurnIsRefusedAndKeepsThisOne(t *testing.T) {
	dir := t.TempDir()
	sessions := dir
	other := session.New(sessions, "s20260101-110000.000", dir)
	if err := other.Append(llm.Message{Role: "user", Content: "another conversation"}); err != nil {
		t.Fatal(err)
	}

	held := &heldServer{release: make(chan struct{})}
	server := httptest.NewServer(held)
	defer func() { held.done(); server.Close() }()

	a := New(llm.NewClient(server.URL+"/v1", ""), nil, "test-model", Options{Root: dir}, &captureSink{})
	a.add(llm.Message{Role: "user", Content: "what this run is saying"})
	go func() { _ = a.Ask(context.Background(), "work") }()
	if !waitUntil(func() bool { return a.turnBusy() }, time.Second) {
		held.done()
		t.Fatal("the turn never marked the run busy")
	}

	if err := a.ContinueFrom(sessions, "s20260101-110000.000"); err == nil {
		t.Error("another conversation was put in this run's place mid-turn")
	}
	got := a.Conversation()
	if len(got) != 2 || got[1].Content != "what this run is saying" {
		t.Errorf("the refused switch changed this run's conversation: %d messages", len(got))
	}
	held.done()
}

func waitUntil(ok func() bool, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if ok() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

// the screen draws from a copy, so repairing stored arguments must not change what the copy shows
func TestAReadersCopyDoesNotMoveUnderIt(t *testing.T) {
	a := newForHistory(Options{})
	// written straight in, as a resumed conversation is, so it reaches the repair instead of bypassing it
	a.history = append(a.history, llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1",
		Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"f`)}}}})

	held := a.Conversation()
	a.sanitizeHistory() // the repair writes into the stored message, in place

	if got := string(held[1].ToolCalls[0].Function.Arguments); got == "{}" {
		t.Error("the copy the reader holds was rewritten by the repair")
	}
	if got := string(a.Conversation()[1].ToolCalls[0].Function.Arguments); got != "{}" {
		t.Errorf("the stored call holds %q, want the repair", got)
	}
}
