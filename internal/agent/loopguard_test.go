package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// the same search asked again is answered from what it already found, so a model hunting the same rock
// pays a notice instead of a fresh result
func TestTheSameSearchIsNotRunTwice(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "search_text", []string{`{"pattern":"port"}`}, "tool_calls"),
		sse("", "", "search_text", []string{`{"pattern":"port"}`}, "tool_calls"),
		sse("", "the port is 8443", "", nil, "stop"),
	}}
	a, done, work, _ := newAuditedAgent(t, stub, Options{MaxTurns: 4, MaxTokens: 512, CtxSize: 8000}, &captureSink{})
	defer done()
	writeLines(t, work, "config.yaml", 4)

	if err := a.Ask(context.Background(), "which port"); err != nil {
		t.Fatal(err)
	}
	replies := toolReplies(t, stub)
	if len(replies) != 2 {
		t.Fatalf("%d tool replies, want one per call:\n%s", len(replies), strings.Join(replies, "\n"))
	}
	if !strings.Contains(replies[1], "already asked") {
		t.Errorf("the repeat was answered like a fresh result:\n%s", replies[1])
	}
	if strings.Contains(replies[0], "already asked") {
		t.Errorf("the first search was told it was a repeat:\n%s", replies[0])
	}
	if !strings.Contains(replies[1], "r1") {
		t.Errorf("the repeat does not point back at the result it already has:\n%s", replies[1])
	}
	// the result was never shortened, so nothing is saved for recall: pointing there would fail
	if strings.Contains(replies[1], "recall") {
		t.Errorf("the repeat sends the model to recall a result that was never saved:\n%s", replies[1])
	}
}

// a repeat notice is only true while nothing has changed: after a write, the same read must read again
func TestAWriteEndsTheRepeats(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"note.txt"}`}, "tool_calls"),
		sse("", "", "write_file", []string{`{"path":"note.txt","content":"port: 9999\n"}`}, "tool_calls"),
		sse("", "", "read_file", []string{`{"path":"note.txt"}`}, "tool_calls"),
		sse("", "done", "", nil, "stop"),
	}}
	sink := &captureSink{decision: Allow}
	a, done, work, _ := newAuditedAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 512, CtxSize: 8000}, sink)
	defer done()
	writeLines(t, work, "note.txt", 3)

	if err := a.Ask(context.Background(), "set the port to 9999"); err != nil {
		t.Fatal(err)
	}
	var seen string
	for _, r := range toolReplies(t, stub) {
		if strings.Contains(r, "note.txt: lines") {
			seen = r
		}
		if strings.Contains(r, "already asked") && strings.Contains(r, "read_file") {
			t.Errorf("a read after a write was refused as a repeat:\n%s", r)
		}
	}
	if !strings.Contains(seen, "9999") {
		t.Errorf("the read after the write did not see the new file:\n%s", seen)
	}
}

// two identical read-only calls in one turn run in parallel, so the guard is touched at the same time
func TestIdenticalCallsInOneTurnAreStillSafe(t *testing.T) {
	args, err := json.Marshal(map[string]string{"pattern": "port"})
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]any{"tool_calls": []map[string]any{
		{"index": 0, "id": "c1", "type": "function", "function": map[string]any{"name": "search_text", "arguments": string(args)}},
		{"index": 1, "id": "c2", "type": "function", "function": map[string]any{"name": "search_text", "arguments": string(args)}},
	}}
	sseBoth := "data: " + mustMarshal(t, map[string]any{"choices": []map[string]any{{"index": 0, "delta": calls, "finish_reason": ""}}}) +
		"\n\ndata: " + mustMarshal(t, map[string]any{"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}}}) +
		"\n\ndata: [DONE]\n\n"
	stub := &stubServer{scripts: []string{
		sseBoth,
		sse("", "the port is 8443", "", nil, "stop"),
	}}
	a, done, work, _ := newAuditedAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 512, CtxSize: 8000}, &captureSink{})
	defer done()
	writeLines(t, work, "config.yaml", 4)

	if err := a.Ask(context.Background(), "which port"); err != nil {
		t.Fatal(err)
	}
	replies := toolReplies(t, stub)
	if len(replies) != 2 {
		t.Fatalf("%d replies for 2 calls:\n%s", len(replies), strings.Join(replies, "\n"))
	}
	notice := 0
	for _, r := range replies {
		if strings.Contains(r, "already asked") {
			notice++
		}
	}
	if notice != 1 {
		t.Errorf("%d of %d identical calls told to stop repeating; one ran, the other should not have", notice, len(replies))
	}
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// a new prompt may follow a change the user made by hand: the same search must run again
func TestANewPromptEndsTheRepeats(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "search_text", []string{`{"pattern":"port"}`}, "tool_calls"),
		sse("", "8443", "", nil, "stop"),
		sse("", "", "search_text", []string{`{"pattern":"port"}`}, "tool_calls"),
		sse("", "9999", "", nil, "stop"),
	}}
	a, done, work, _ := newAuditedAgent(t, stub, Options{MaxTurns: 4, MaxTokens: 512, CtxSize: 8000}, &captureSink{})
	defer done()
	writeLines(t, work, "config.yaml", 4)

	for _, prompt := range []string{"which port", "I changed it, which port now"} {
		if err := a.Ask(context.Background(), prompt); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range toolReplies(t, stub) {
		if strings.Contains(r, "already asked") {
			t.Errorf("a search in a new prompt was refused as a repeat:\n%s", r)
		}
	}
}

// read_file answers its own repeats by content, so the guard must not refuse a re-read: a shortened read
// tells the model to read the range again, and that has to work
func TestTheGuardLeavesReadsToReadFile(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"note.txt"}`}, "tool_calls"),
		sse("", "", "read_file", []string{`{"path":"note.txt"}`}, "tool_calls"),
		sse("", "done", "", nil, "stop"),
	}}
	a, done, work, _ := newAuditedAgent(t, stub, Options{MaxTurns: 4, MaxTokens: 512, CtxSize: 8000}, &captureSink{})
	defer done()
	writeLines(t, work, "note.txt", 3)

	if err := a.Ask(context.Background(), "read note.txt"); err != nil {
		t.Fatal(err)
	}
	replies := toolReplies(t, stub)
	if strings.Contains(replies[1], "already asked") {
		t.Errorf("the loop guard answered a read_file repeat; read_file's own check should:\n%s", replies[1])
	}
}

// once a result leaves the conversation, asking for it again is the only way back to the text
func TestAForgottenResultCanBeAskedForAgain(t *testing.T) {
	g := newLoopGuard()
	if earlier, _ := g.claim("search_text a", "r1", 1); earlier != nil {
		t.Fatal("the first call was called a repeat")
	}
	if earlier, _ := g.claim("search_text a", "r2", 2); earlier == nil {
		t.Fatal("the second call was not caught")
	}
	g.forgetID("r1")
	if earlier, times := g.claim("search_text a", "r3", 3); earlier != nil || times != 1 {
		t.Fatalf("after r1 was dropped the call is still a repeat (earlier %v, attempt %d)", earlier, times)
	}
}

// shortening an old search result releases it from the guard, end to end
func TestShorteningAResultReleasesTheGuard(t *testing.T) {
	a, done, _, _ := newAuditedAgent(t, &stubServer{scripts: []string{sse("", "x", "", nil, "stop")}}, Options{MaxTurns: 2, CtxSize: 8000}, &captureSink{})
	defer done()
	a.repeats.claim("search_text {}", "r1", 1)
	a.forget(llm.Message{Role: "tool", Name: "search_text", CallID: "r1", Content: "old"})
	if earlier, _ := a.repeats.claim("search_text {}", "r2", 2); earlier != nil {
		t.Fatal("a result the conversation dropped still counts as in front of the model")
	}
}

// a model that keeps asking the same thing is stopped and asked for an answer, long before max-turns
func TestAStuckRunIsStoppedWithAnAnswer(t *testing.T) {
	search := sse("", "", "search_text", []string{`{"pattern":"port"}`}, "tool_calls")
	stub := &stubServer{scripts: []string{search, search, search, search, sse("", "8443", "", nil, "stop")}}
	sink := &captureSink{}
	a, done, work, _ := newAuditedAgent(t, stub, Options{MaxTurns: 25, MaxTokens: 512, CtxSize: 8000}, sink)
	defer done()
	writeLines(t, work, "config.yaml", 4)

	if err := a.Ask(context.Background(), "which port"); err != nil {
		t.Fatalf("a stuck run that can still answer should not fail: %v", err)
	}
	if n := len(stub.requests); n != 5 {
		t.Errorf("%d requests, want 4 searches and one answer-only request", n)
	}
	if tools, ok := stub.lastRequest(t)["tools"]; ok {
		t.Errorf("the answer-only request still offers tools: %v", tools)
	}
	if got := sink.content.String(); got != "8443" {
		t.Errorf("answer = %q", got)
	}
	replies := toolReplies(t, stub)
	if !strings.Contains(replies[2], "ends the run") {
		t.Errorf("the third ask was not warned that the next one stops the run:\n%s", replies[2])
	}
}
