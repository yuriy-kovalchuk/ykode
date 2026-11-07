package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

type stubServer struct {
	requests []map[string]any
	scripts  []string
}

func (s *stubServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

// captureSink records what the renderer was given.
// captureSink collects the event stream so tests can assert on what a consumer would see.

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
	registry := tools.NewRegistry(
		tools.NewFindFiles(root, dir),
		tools.NewReadFile(root, dir),
		tools.NewWriteFile(root, dir),
	)
	a := New(llm.NewClient(server.URL+"/v1", ""), registry, "test-model", opts, sink)
	return a, func() { server.Close(); root.Close() }
}

func TestMaxTurnsStopsTheLoop(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "", "find_files", []string{`{"pattern":"*","depth":1}`}, "tool_calls")}}
	a, done := newAgent(t, stub, Options{MaxTurns: 2, MaxTokens: 512}, &captureSink{})
	defer done()

	err := a.Ask(context.Background(), "keep going")
	if err == nil || !strings.Contains(err.Error(), "reached --max-turns (2)") {
		t.Fatalf("error = %v, want the max-turns failure", err)
	}
	if len(stub.requests) != 2 {
		t.Fatalf("%d requests, want one per tool round", len(stub.requests))
	}
}

func TestAnswerWithNoToolCallIsVerifiedOnce(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "the file is fine", "", nil, "stop"),
		sse("", "", "find_files", []string{`{"pattern":"*","depth":1}`}, "tool_calls"),
		sse("", "checked: the file is fine", "", nil, "stop"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 512}, &captureSink{})
	defer done()

	if err := a.Ask(context.Background(), "why does main.go fail to build?"); err != nil {
		t.Fatal(err)
	}
	if len(stub.requests) != 3 {
		t.Fatalf("%d requests, want: answer, verification retry with a tool, final answer", len(stub.requests))
	}
	if got := stub.requests[0]["tool_choice"]; got != "auto" {
		t.Fatalf("first request tool_choice = %v, want auto", got)
	}
	if got := stub.requests[1]["tool_choice"]; got != "required" {
		t.Fatalf("verification request tool_choice = %v, want required", got)
	}
	if !hasUserNote(stub.requests[1], "without calling any tool") {
		t.Fatalf("verification request has no nudge:\n%v", stub.requests[1]["messages"])
	}
}

func TestToolUsingRequestIsNotNudged(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "find_files", []string{`{"pattern":"*","depth":1}`}, "tool_calls"),
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

func hasUserNote(req map[string]any, want string) bool {
	messages, _ := req["messages"].([]any)
	for _, m := range messages {
		entry, _ := m.(map[string]any)
		if entry["role"] == "user" && strings.Contains(fmt.Sprint(entry["content"]), want) {
			return true
		}
	}
	return false
}

func TestAMemoryAnswerIsCheckedAgainAtMostOnce(t *testing.T) {
	cases := []struct {
		name, prompt string
		noVerify     bool
		requests     int
	}{
		{"a question about the agent is not nudged", "what model are you and how fast are you?", false, 1},
		{"the verification retry does not loop", "fix the failing test", false, 2},
		{"--no-verify turns the nudge off", "why does main.go fail to build?", true, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := &stubServer{scripts: []string{sse("", "answered from memory", "", nil, "stop")}}
			a, done := newAgent(t, stub, Options{MaxTurns: 8, MaxTokens: 512, NoVerify: c.noVerify}, &captureSink{})
			defer done()
			if err := a.Ask(context.Background(), c.prompt); err != nil {
				t.Fatal(err)
			}
			if len(stub.requests) != c.requests {
				t.Errorf("%d requests, want %d", len(stub.requests), c.requests)
			}
		})
	}
}

func TestTheNudgeMatchesWholeWordsOnly(t *testing.T) {
	for _, ask := range []string{"fix the bug in main.go", "add a test", "why does this fail", "review this file"} {
		if !looksLikeProjectWork(ask) {
			t.Errorf("%q should read as project work", ask)
		}
	}
	for _, ask := range []string{"address the feedback", "update my profile", "what model are you?", "how do I use you?"} {
		if looksLikeProjectWork(ask) {
			t.Errorf("%q is not project work", ask)
		}
	}
}

func TestReasoningIsSentBackWhenTheTemplateWantsIt(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("thinking first", "", "find_files", []string{`{"pattern":"*","depth":1}`}, "tool_calls"),
		sse("", "done", "", nil, "stop"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 512, SendReasoning: true}, &captureSink{})
	defer done()

	if err := a.Ask(context.Background(), "list the files"); err != nil {
		t.Fatal(err)
	}
	if got := assistantReasoning(t, stub.lastRequest(t)); got != "thinking first" {
		t.Fatalf("reasoning_content in the follow-up = %q, want the earlier thinking back", got)
	}
}

func TestThinkingIsNotSentBackUnasked(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("thinking first", "", "find_files", []string{`{"pattern":"*","depth":1}`}, "tool_calls"),
		sse("", "done", "", nil, "stop"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 512}, &captureSink{})
	defer done()

	if err := a.Ask(context.Background(), "list the files"); err != nil {
		t.Fatal(err)
	}
	if got := assistantReasoning(t, stub.lastRequest(t)); got != "" {
		t.Fatalf("reasoning_content in the follow-up = %q, want it left out by default", got)
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
