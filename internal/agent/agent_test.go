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
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// stubServer replays scripted SSE bodies and records every request body.
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

// sse builds a streaming reply: thinking fragments, answer fragments, then tool
// call fragments with the id and name only in the first one.
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
type captureSink struct {
	reasoning strings.Builder
	content   strings.Builder
	stats     []Stats
}

func (c *captureSink) Reasoning(chunk string) { c.reasoning.WriteString(chunk) }
func (c *captureSink) Content(chunk string)   { c.content.WriteString(chunk) }
func (c *captureSink) TurnDone(stats Stats)   { c.stats = append(c.stats, stats) }

func newAgent(t *testing.T, stub *stubServer, opts Options, sink Sink) (*Agent, func()) {
	t.Helper()
	server := httptest.NewServer(stub)
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	registry := tools.NewRegistry(
		tools.NewListDir(root, dir),
		tools.NewReadFile(root, dir),
		tools.NewWriteFile(root, dir),
	)
	a := New(llm.NewClient(server.URL+"/v1"), registry, "test-model", opts, sink, io.Discard)
	return a, func() { server.Close(); root.Close() }
}

func TestFirstRequestCarriesSystemPrompt(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("thinking hard", "the answer", "", nil, "stop")}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 512}, sink)
	defer done()

	if err := a.Ask(context.Background(), "do it"); err != nil {
		t.Fatal(err)
	}
	messages := stub.lastRequest(t)["messages"].([]any)
	first := messages[0].(map[string]any)
	if first["role"] != "system" || !strings.Contains(first["content"].(string), "ykode") {
		t.Fatalf("first message = %v, want the system prompt", first)
	}
	if sink.reasoning.String() != "thinking hard" || sink.content.String() != "the answer" {
		t.Fatalf("sink got reasoning=%q content=%q", sink.reasoning.String(), sink.content.String())
	}
}

func TestMaxTokensFitsTheServerContext(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "ok", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{MaxTurns: 1, MaxTokens: 4096, CtxSize: 4096}, &captureSink{})
	defer done()
	if err := a.Ask(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	got := int(stub.lastRequest(t)["max_tokens"].(float64))
	if got >= 4096 || got < 3000 {
		t.Fatalf("max_tokens = %d, want it reduced below the 4096 context but still usable", got)
	}
}

func TestContextTooSmallFailsBeforeRequesting(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "ok", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{MaxTurns: 1, MaxTokens: 512, CtxSize: 120}, &captureSink{})
	defer done()

	err := a.Ask(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("err = %v, want a context-window error", err)
	}
	if len(stub.requests) != 0 {
		t.Fatalf("made %d requests, want none", len(stub.requests))
	}
}

func TestTruncatedToolCallIsNotRun(t *testing.T) {
	// The arguments stop mid-string: running this would send broken JSON.
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":`}, "length"),
		sse("", "recovered", "", nil, "stop"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 512}, &captureSink{})
	defer done()

	if err := a.Ask(context.Background(), "read it"); err != nil {
		t.Fatal(err)
	}
	if len(stub.requests) != 2 {
		t.Fatalf("requests = %d, want the loop to continue to a second turn", len(stub.requests))
	}
	roles := stub.lastRequest(t)["messages"].([]any)
	var note string
	for _, raw := range roles {
		m := raw.(map[string]any)
		if m["role"] == "assistant" && strings.Contains(fmt.Sprint(m["content"]), "cut off") {
			note = m["content"].(string)
		}
	}
	if note == "" {
		t.Fatal("the model was never told its reply was cut off")
	}
}

func TestOutsideRootIsConfirmedEvenWithAssumeYes(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "outside.txt"), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"../outside.txt"}`}, "tool_calls"),
		sse("", "fine", "", nil, "stop"),
	}}
	var asked []string
	opts := Options{MaxTurns: 3, MaxTokens: 512, AssumeYes: true, Confirm: func(what string) bool {
		asked = append(asked, what)
		return false
	}}
	server := httptest.NewServer(stub)
	defer server.Close()
	root, err := os.OpenRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	a := New(llm.NewClient(server.URL+"/v1"),
		tools.NewRegistry(tools.NewReadFile(root, project)), "test-model", opts, &captureSink{}, io.Discard)

	if err := a.Ask(context.Background(), "read the file above the root"); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || !strings.Contains(asked[0], filepath.Join(dir, "outside.txt")) {
		t.Fatalf("confirmation asked = %v, want one question naming the resolved outside path", asked)
	}
	// The refusal must reach the model as the tool result.
	var refusal string
	for _, raw := range stub.lastRequest(t)["messages"].([]any) {
		m := raw.(map[string]any)
		if m["role"] == "tool" {
			refusal = fmt.Sprint(m["content"])
		}
	}
	if !strings.Contains(refusal, "refused") {
		t.Fatalf("tool result = %q, want the refusal", refusal)
	}
}

func TestMaxTurnsStopsTheLoop(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "", "list_dir", []string{`{"path":"."}`}, "tool_calls")}}
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
