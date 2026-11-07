package agent

import (
	"context"
	"strings"
	"testing"
)

// a run that spent its turn budget still owes an answer: the last request has no tools, so text is
// all the model can produce
func TestARunOutOfTurnsIsForcedToAnswer(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "find_files", []string{`{"pattern":"*.yaml"}`}, "tool_calls"),
		sse("", "", "find_files", []string{`{"pattern":"*.yaml"}`}, "tool_calls"),
		sse("", "8443", "", nil, "stop"),
	}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 2, MaxTokens: 512, CtxSize: 8000}, sink)
	defer done()

	if err := a.Ask(context.Background(), "which port does production use"); err != nil {
		t.Fatalf("a run that can still answer should not fail: %v", err)
	}
	if got := sink.content.String(); got != "8443" {
		t.Errorf("answer = %q, want the number the run had already found", got)
	}
	last := stub.lastRequest(t)
	if tools, ok := last["tools"]; ok {
		t.Errorf("the answer-only request still offers tools: %v", tools)
	}
	if !sink.noticed("answered from what it had") {
		t.Error("the run never said it stopped early and answered anyway")
	}
}

// the last request can still come back empty: then the max-turns error is the honest report
func TestAnEmptyForcedAnswerStillReportsTheTurnLimit(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "find_files", []string{`{"pattern":"*.yaml"}`}, "tool_calls"),
		sse("", "", "find_files", []string{`{"pattern":"*.yaml"}`}, "tool_calls"),
		sse("", "", "find_files", []string{`{"pattern":"*.yaml"}`}, "tool_calls"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 2, MaxTokens: 512, CtxSize: 8000}, &captureSink{})
	defer done()

	err := a.Ask(context.Background(), "which port does production use")
	if err == nil || !strings.Contains(err.Error(), "reached --max-turns (2)") {
		t.Fatalf("error = %v, want the turn limit reported", err)
	}
}

// a reply cut off by max_tokens, every time, must still stop at the turn limit
func TestCutOffCallsStillStopAtMaxTurns(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "find_files", []string{`{"pattern":"*.ya`}, "length"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 512, CtxSize: 8000}, &captureSink{})
	defer done()

	err := a.Ask(context.Background(), "which port")
	if err == nil || !strings.Contains(err.Error(), "reached --max-turns (3)") {
		t.Fatalf("error = %v, want the turn limit", err)
	}
	if n := len(stub.requests); n != 4 {
		t.Errorf("%d requests, want 3 turns and one answer-only request", n)
	}
}

// with no tools offered a template can still produce a call; nothing runs it, so it must not stay in
// the history without a reply
func TestAForcedAnswerKeepsNoToolCall(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "find_files", []string{`{"pattern":"*.yaml"}`}, "tool_calls"),
		sse("", "8443", "find_files", []string{`{"pattern":"*.yaml"}`}, "stop"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 1, MaxTokens: 512, CtxSize: 8000}, &captureSink{})
	defer done()

	if err := a.Ask(context.Background(), "which port"); err != nil {
		t.Fatal(err)
	}
	last := a.history[len(a.history)-1]
	if last.Role != "assistant" || len(last.ToolCalls) != 0 || last.Content != "8443" {
		t.Fatalf("last message = %+v, want the text answer with no call", last)
	}
}
