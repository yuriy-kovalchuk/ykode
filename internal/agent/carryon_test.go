package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// asksSent are the user messages the run added on its own, besides the ask it was answering.
func asksSent(a *Agent) []string {
	var notes []string
	for _, m := range a.history {
		if m.Role == "user" && m.Content != a.task {
			notes = append(notes, m.Content)
		}
	}
	return notes
}

// a call the model wrote as text, whole and naming a tool this run has, is run as the call it is: a
// model whose server does not read its markup back would otherwise stop after every second call
func TestACallWrittenAsTextRuns(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "Let me write it.\n<tool_call>\n<function=write_file>\n<parameter=path>\nnote.txt\n</parameter>\n<parameter=content>\nline one\nline two\n</parameter>\n</function>\n</tool_call>", "", nil, "stop"),
		sse("", "<tool_call>\n{\"name\": \"read_file\", \"arguments\": {\"path\": \"note.txt\"}}\n</tool_call>", "", nil, "stop"),
		sse("", "It is written.", "", nil, "stop"),
	}}
	sink := &captureSink{decision: Allow}
	a, done := newAgent(t, stub, Options{MaxTurns: 6, MaxTokens: 256, AssumeYes: true}, sink)
	defer done()
	if err := a.Ask(context.Background(), "write the note"); err != nil {
		t.Fatal(err)
	}
	var results []string
	for _, m := range a.history {
		if m.Role == "tool" {
			results = append(results, m.Name+": "+m.Content)
		}
		if m.Role == "assistant" && hasTextCall(m.Content) {
			t.Errorf("the markup stayed in the conversation: %q", m.Content)
		}
	}
	if len(results) != 2 || !strings.Contains(results[0], "write_file: created note.txt") || !strings.Contains(results[1], "line one\nline two") {
		t.Fatalf("the two calls did not both run with their arguments: %q", results)
	}
	if shown := sink.content.String(); strings.Contains(shown, "<") || !strings.Contains(shown, "Let me write it.") || !strings.Contains(shown, "It is written.") {
		t.Errorf("the screen was handed the markup, or lost the words around it: %q", shown)
	}
	if len(asksSent(a)) != 0 {
		t.Errorf("the model was told off for a call that ran: %q", asksSent(a))
	}
}

// the markup of a call is held back however the stream cuts it up, and words that only look like
// the start of a tag come through
func TestTheMarkupOfACallIsNeverShown(t *testing.T) {
	answer := "a < b and <b>bold</b>, then <tool_call>\n<function=read_file>\n</function>\n</tool_call> done <function=x>y</function>."
	for size := 1; size <= len(answer); size++ {
		var h callHider
		var shown strings.Builder
		for at := 0; at < len(answer); at += size {
			shown.WriteString(h.feed(answer[at:min(at+size, len(answer))]))
		}
		shown.WriteString(h.rest())
		if got, want := shown.String(), "a < b and <b>bold</b>, then  done ."; got != want {
			t.Fatalf("in pieces of %d: shown %q, want %q", size, got, want)
		}
	}
	var open callHider
	if got := open.feed("before <tool_call>{\"name\":") + open.rest(); got != "before " {
		t.Errorf("a call that never closed was shown: %q", got)
	}
}

// a reply cut off at max_tokens ends the ask, as it does in pi: the user decides whether to go on
func TestAReplyCutOffEndsTheAsk(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "The first half of the", "", nil, "length")}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 8, MaxTokens: 64}, sink)
	defer done()
	if err := a.Ask(context.Background(), "explain it"); err != nil {
		t.Fatal(err)
	}
	if len(stub.requests) != 1 || len(asksSent(a)) != 0 || !sink.noticed("hit max_tokens") {
		t.Fatalf("%d requests, notes %q: want one request, the cut reported, and nothing sent in the user's voice",
			len(stub.requests), asksSent(a))
	}
}

// a reply with no tool call is the answer, whatever it says next and whatever the plan still holds
func TestAReplyWithNoCallEndsTheAsk(t *testing.T) {
	plan := tools.NewPlan()
	plan.Restore([]tools.PlanStep{{Text: "read", Status: tools.PlanDone}, {Text: "write", Status: tools.PlanActive}})
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"a.go"}`}, "tool_calls"),
		sse("", "The files are listed.\n\nNow let me read the first one", "", nil, "stop"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 8, MaxTokens: 64, Plan: plan}, &captureSink{})
	defer done()
	if err := a.Ask(context.Background(), "look around"); err != nil {
		t.Fatal(err)
	}
	if len(stub.requests) != 2 || len(asksSent(a)) != 0 {
		t.Errorf("%d requests, notes %q: want the reply taken as it is", len(stub.requests), asksSent(a))
	}
}

// tool results carry what the tool returned and nothing else: a line the program slipped into them
// read to the model as an injected instruction
func TestAToolResultCarriesOnlyTheToolsOutput(t *testing.T) {
	plan := tools.NewPlan()
	scripts := []string{sse("", "", "plan", []string{`{"steps":[{"text":"look","status":"active"},{"text":"answer"}]}`}, "tool_calls")}
	for i := 0; i < 8; i++ {
		scripts = append(scripts, sse("", "", "read_file", []string{`{"path":"f` + string(rune('a'+i)) + `.go"}`}, "tool_calls"))
	}
	scripts = append(scripts, sse("", "done", "", nil, "stop"))
	stub := &stubServer{scripts: scripts}
	a, done := newAgent(t, stub, Options{MaxTurns: 20, MaxTokens: 64, Plan: plan}, &captureSink{})
	defer done()
	a.tools = tools.NewRegistry(append(a.tools.All(), plan)...)
	if err := a.Ask(context.Background(), "look around"); err != nil {
		t.Fatal(err)
	}
	for _, m := range a.history {
		if m.Role == "tool" && strings.Contains(m.Content, "[ykode") {
			t.Errorf("a result carries a line from the program: %q", m.Content)
		}
	}
}

// out of rounds, the run stops where it is: no extra request, and a history the next ask can follow
func TestOutOfRoundsStopsWithoutAnotherRequest(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "", "read_file", []string{`{"path":"a.go"}`}, "tool_calls")}}
	a, done := newAgent(t, stub, Options{MaxTurns: 1, MaxTokens: 64}, &captureSink{})
	defer done()
	err := a.Ask(context.Background(), "look around")
	if err == nil || !strings.Contains(err.Error(), "reached --max-turns (1)") || !strings.Contains(err.Error(), "carry on") {
		t.Fatalf("error = %v, want the limit and how to go on", err)
	}
	if len(stub.requests) != 1 || len(asksSent(a)) != 0 {
		t.Errorf("%d requests, notes %q: want the one round and nothing more", len(stub.requests), asksSent(a))
	}
	if last := a.history[len(a.history)-1]; last.Role != "tool" {
		t.Errorf("the history ends on %s, want the call's result", last.Role)
	}
}

// a server started with no flags has a window of four thousand tokens and ykode's default margin is
// as many: the reserve gives way, and a turn still completes
func TestATurnCompletesInASmallWindow(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{CtxSize: 4096, SafetyMargin: 4096, MaxTurns: 4}, &captureSink{})
	defer done()
	if err := a.Ask(context.Background(), "say something"); err != nil {
		t.Fatalf("a window of 4096 with a margin of 4096 cannot answer: %v", err)
	}
	if got := stub.lastRequest(t)["max_tokens"].(float64); got < minAnswerTok {
		t.Errorf("the reply was left %v tokens", got)
	}
}

// whatever the drop leaves, the request that follows it can go: the mark it drops to and the check a
// request makes are one rule
func TestWhatDroppingLeavesCanBeSent(t *testing.T) {
	for _, window := range []int{2048, 4096, 8192, 32768} {
		a := New(nil, nil, "test-model", Options{CtxSize: window, SafetyMargin: 4096}, nil)
		a.history = append(a.history, llm.Message{Role: "user", Content: "read everything"})
		for i := 0; i < 40; i++ {
			id := fmt.Sprintf("c%d", i)
			a.history = append(a.history,
				llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id, Type: "function", Function: llm.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}}}},
				llm.Message{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("a line of the file that was read\n", 60)})
		}
		a.makeRoom()
		if _, err := a.request(); err != nil {
			t.Errorf("window %d: dropped to, then refused: %v", window, err)
		}
	}
}

// a server that says the request does not fit is believed: room is made and the turn asked again
func TestAnOverflowIsRecoveredFromOnce(t *testing.T) {
	asked := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if asked++; asked == 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"code":400,"message":"the request exceeds the available context size, try increasing it","type":"exceed_context_size_error"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse("", "it fits now", "", nil, "stop"))
	}))
	defer server.Close()
	sink := &captureSink{}
	a := New(llm.NewClient(server.URL+"/v1", ""), tools.NewRegistry(), "test-model", Options{CtxSize: 8000, MaxTurns: 4}, sink)
	before := a.meter.bytesPerTok
	if err := a.Ask(context.Background(), "say something"); err != nil {
		t.Fatalf("the overflow ended the turn: %v", err)
	}
	if asked != 2 || a.meter.bytesPerTok >= before || !strings.Contains(sink.content.String(), "it fits now") {
		t.Errorf("%d requests, ratio %.2f -> %.2f, answer %q", asked, before, a.meter.bytesPerTok, sink.content.String())
	}
}

// a conversation that was folded before it was saved does not fold again the moment it comes back
func TestAResumedFoldWaitsForNewWork(t *testing.T) {
	a := New(nil, nil, "test-model", Options{}, nil)
	fresh := New(nil, nil, "test-model", Options{}, nil)
	fresh.Resume([]llm.Message{{Role: "user", Content: "hello"}})
	a.Resume([]llm.Message{{Role: "user", Content: FoldMark + "\n## Goal\nfinish it"}, {Role: "user", Content: "carry on"}})
	if !fresh.foldOverdue() || a.foldOverdue() {
		t.Errorf("overdue: never folded %v, folded before it was saved %v", fresh.foldOverdue(), a.foldOverdue())
	}
}

// an overflow that cannot be folded drops the oldest messages and logs each drop once: the session
// file is not written out whole again on top of the drops
func TestAnOverflowDropIsLoggedOnce(t *testing.T) {
	asked := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if asked++; asked == 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"code":400,"message":"the request exceeds the available context size, try increasing it"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse("", "it fits now", "", nil, "stop"))
	}))
	defer server.Close()
	dir := t.TempDir()
	saved := session.New(dir, "s20260101-120000", dir)
	a := New(llm.NewClient(server.URL+"/v1", ""), tools.NewRegistry(), "test-model",
		Options{CtxSize: 8000, MaxTurns: 4, Session: saved}, &captureSink{})
	a.add(llm.Message{Role: "user", Content: "an older ask"})
	a.add(llm.Message{Role: "assistant", Content: "its answer"})
	if err := a.Ask(context.Background(), "say something"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "s20260101-120000.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"removed"`) || strings.Contains(string(raw), `"rewritten"`) {
		t.Errorf("the drops are not logged once each:\n%s", raw)
	}
}
