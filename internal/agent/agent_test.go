package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

type stubServer struct {
	requests []map[string]any
	scripts  []string
}

func (s *stubServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// the model list is a GET with no body: scripting it shifts later requests onto the wrong script
	if strings.HasSuffix(r.URL.Path, "/models") {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"test-model","context_length":8000},{"id":"other-model","context_length":16000}]}`)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		panic(fmt.Sprintf("server got unmarshalable request: %v", err))
	}
	s.requests = append(s.requests, req)
	w.Header().Set("Content-Type", "text/event-stream")
	index := min(len(s.requests)-1, len(s.scripts)-1)
	fmt.Fprint(w, s.scripts[index])
}

func (s *stubServer) lastRequest(t *testing.T) map[string]any {
	t.Helper()
	if len(s.requests) == 0 {
		t.Fatal("the agent made no request")
	}
	return s.requests[len(s.requests)-1]
}

// sse builds a streaming reply: thinking, answer, then tool call fragments keyed by index.

func sse(reasoning, content string, tool string, argsParts []string, finish string) string {
	var b strings.Builder
	chunk := func(delta map[string]any, finish string) {
		payload := map[string]any{"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}}}
		raw, err := json.Marshal(payload)
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(&b, "data: %s\n\n", raw)
	}
	if reasoning != "" {
		chunk(map[string]any{"reasoning_content": reasoning}, "")
	}
	for i, part := range argsParts {
		call := map[string]any{"index": 0, "function": map[string]any{"arguments": part}}
		if i == 0 {
			call["id"] = "call1"
			call["type"] = "function"
			call["function"].(map[string]any)["name"] = tool
		}
		chunk(map[string]any{"tool_calls": []map[string]any{call}}, "")
	}
	if content != "" {
		chunk(map[string]any{"content": content}, "")
	}
	chunk(map[string]any{}, finish)
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

// captureSink collects the event stream so tests can assert on what a consumer would see

type captureSink struct {
	mu        sync.Mutex // tools can run at the same time, so the sink sees them concurrently
	events    []Event
	reasoning strings.Builder
	content   strings.Builder
	stats     []Stats
	asked     []Event
	decision  Decision // what this sink answers a Confirm with
}

func (c *captureSink) Emit(ev Event) Decision {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
	switch ev.Kind {
	case Thinking:
		c.reasoning.WriteString(ev.Text)
	case Answer:
		c.content.WriteString(ev.Text)
	case TurnEnd:
		if ev.Stats != (Stats{}) {
			c.stats = append(c.stats, ev.Stats)
		}
	case Confirm:
		c.asked = append(c.asked, ev)
	}
	return c.decision
}

// noticed says whether a notice containing text was emitted.
func (c *captureSink) noticed(text string) bool {
	for _, ev := range c.events {
		if ev.Kind == Notice && strings.Contains(ev.Text, text) {
			return true
		}
	}
	return false
}

// kinds lists the event kinds in order, for asserting the shape of a run.

func (c *captureSink) kinds() []EventKind {
	kinds := make([]EventKind, 0, len(c.events))
	for _, ev := range c.events {
		kinds = append(kinds, ev.Kind)
	}
	return kinds
}

func newAgent(t *testing.T, stub *stubServer, opts Options, sink Sink) (*Agent, func()) {
	t.Helper()
	server := httptest.NewServer(stub)
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seeded.txt"), []byte("one line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := tools.NewRegistry(
		tools.NewReadFile(root, dir),
		tools.NewWriteFile(root, dir),
	)
	a := New(llm.NewClient(server.URL+"/v1"), registry, "test-model", opts, sink)
	return a, func() { server.Close(); root.Close() }
}

// newAgentIn is newBuiltInAgent over a test directory, so the test can fill it before the run reads
func newAgentIn(t *testing.T, stub *stubServer, opts Options, sink Sink, dir string) (*Agent, func()) {
	t.Helper()
	server := httptest.NewServer(stub)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := New(llm.NewClient(server.URL+"/v1"), tools.BuiltIn(root, dir, tools.Options{MaxResultBytes: 4000}),
		"test-model", opts, sink)
	return a, func() { server.Close(); root.Close() }
}

// writeLines makes a text file of n lines, each long enough that reading it costs real bytes.
func writeLines(t *testing.T, dir, name string, n int) string {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString("line " + strconv.Itoa(i) + " of " + name + " carrying enough text to matter\n")
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

// newBuiltInAgent is an agent over every built-in tool, its root a scratch dir it also returns
func newBuiltInAgent(t *testing.T, stub *stubServer, opts Options, sink Sink) (*Agent, string) {
	t.Helper()
	server := httptest.NewServer(stub)
	t.Cleanup(server.Close)
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	a := New(llm.NewClient(server.URL+"/v1"), tools.BuiltIn(root, dir, tools.Options{MaxResultBytes: 4000}),
		"test-model", opts, sink)
	a.opts.Root = dir
	return a, dir
}

func TestMaxTurnsStopsTheLoop(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "", "read_file", []string{`{"path":"a.go"}`}, "tool_calls")}}
	a, done := newAgent(t, stub, Options{MaxTurns: 2, MaxTokens: 512}, &captureSink{})
	defer done()

	err := a.Ask(context.Background(), "keep going")
	if err == nil || !strings.Contains(err.Error(), "reached --max-turns (2)") {
		t.Fatalf("error = %v, want the max-turns failure", err)
	}
	if len(stub.requests) != 2 {
		t.Fatalf("%d requests, want one per tool round and nothing after", len(stub.requests))
	}
}

func TestToolUsingRequestIsNotNudged(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"a.go"}`}, "tool_calls"),
		sse("", "one directory", "", nil, "stop"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 512}, &captureSink{})
	defer done()

	if err := a.Ask(context.Background(), "what files are here?"); err != nil {
		t.Fatal(err)
	}
	if len(stub.requests) != 2 {
		t.Fatalf("%d requests, want the answer to stand after one tool round", len(stub.requests))
	}
}

// hasUserNote looks for text in the user messages of a recorded request.

// the model sees its own thinking on the next tool round, as pi sends it
func TestReasoningIsAlwaysSentBack(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("thinking first", "", "read_file", []string{`{"path":"a.go"}`}, "tool_calls"),
		sse("", "done", "", nil, "stop"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 512}, &captureSink{})
	defer done()

	if err := a.Ask(context.Background(), "list the files"); err != nil {
		t.Fatal(err)
	}
	if got := assistantReasoning(t, stub.lastRequest(t)); got != "thinking first" {
		t.Fatalf("reasoning_content in the follow-up = %q, want the earlier thinking back", got)
	}
}

// assistantReasoning reads the thinking a server saw on the assistant turn before this request.

func assistantReasoning(t *testing.T, request map[string]any) string {
	t.Helper()
	for _, entry := range request["messages"].([]any) {
		msg := entry.(map[string]any)
		if msg["role"] == "assistant" {
			got, _ := msg["reasoning_content"].(string)
			return got
		}
	}
	return ""
}

// starting over has to reach the saved conversation too, or --continue hands back what was cleared
func TestStartingOverClearsTheSavedConversation(t *testing.T) {
	dir := t.TempDir()
	saved := session.New(dir, "s20260101-120000", dir)
	a, done := newAgentIn(t, &stubServer{}, Options{Session: saved}, &captureSink{}, dir)
	defer done()

	a.add(llm.Message{Role: "user", Content: "an ask that will be cleared"})
	a.add(llm.Message{Role: "assistant", Content: "and its answer"})
	if _, _, err := a.Command(context.Background(), "/reset"); err != nil {
		t.Fatal(err)
	}

	back, err := session.Load(filepath.Join(dir, "s20260101-120000.jsonl"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 0 {
		t.Errorf("a resume reads back what was cleared: %d messages", len(back))
	}
}
