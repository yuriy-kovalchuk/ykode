package agent

import (
	"context"
	"strings"
	"testing"
)

// the loop that hung a benchmark run for 600 s: the same few lines, over and over
const loopParagraph = "Let me re-read the file to get the exact text.\n\n" +
	"Wait, I just realized - the read_file output shows something else.\n\n" +
	"OK, I think I need to just re-read the file and use the exact text.\n\n"

func TestALoopingReplyIsCaught(t *testing.T) {
	w := newLineRepeats()
	stuck := false
	// streamed in small pieces, the way a server sends it
	text := strings.Repeat(loopParagraph, stuckLineLimit)
	for i := 0; i < len(text); i += 7 {
		if w.add(text[i:min(i+7, len(text))]) {
			stuck = true
			break
		}
	}
	if !stuck {
		t.Fatal("a paragraph repeated ten times was not caught")
	}
}

func TestHonestTextIsNotCaught(t *testing.T) {
	w := newLineRepeats()
	var b strings.Builder
	// code repeats short lines all the time, and a plan repeats its shape but not its words
	for i := 0; i < 40; i++ {
		b.WriteString("\tif err != nil {\n\t\treturn err\n\t}\n}\n")
		b.WriteString("- step " + strings.Repeat("x", i) + ": read the next file and check its imports\n")
	}
	b.WriteString(strings.Repeat("Let me check the file again.\n", stuckLineLimit-1))
	if w.add(b.String()) {
		t.Error("ordinary text was taken for a loop")
	}
}

func loopingThought(t *testing.T) string {
	t.Helper()
	return sse(strings.Repeat(loopParagraph, stuckLineLimit+5), "", "", nil, "stop")
}

// a looping reply is stopped, the model is told to move on, and the run goes on to an answer
func TestALoopingReplyIsStoppedAndTheRunGoesOn(t *testing.T) {
	stub := &stubServer{scripts: []string{
		loopingThought(t),
		sse("", "8443", "", nil, "stop"),
	}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 4, MaxTokens: 512, CtxSize: 8000}, sink)
	defer done()

	if err := a.Ask(context.Background(), "which port"); err != nil {
		t.Fatal(err)
	}
	if !sink.noticed("kept repeating the same lines") {
		t.Error("the user was not told the reply was stopped")
	}
	if got := sink.content.String(); got != "8443" {
		t.Errorf("answer = %q", got)
	}
	last := stub.lastRequest(t)["messages"].([]any)
	note := last[len(last)-1].(map[string]any)
	if note["role"] != "user" || !strings.Contains(note["content"].(string), "kept repeating") {
		t.Errorf("the next request does not tell the model why it was stopped: %v", note)
	}
	for _, m := range a.history {
		if strings.Contains(m.Content, "Let me re-read the file") {
			t.Error("the looping text was kept in the history")
		}
	}
}

// a second loop in the same task is not stopped and retried forever: the run answers from what it has
func TestASecondLoopEndsWithAnAnswer(t *testing.T) {
	stub := &stubServer{scripts: []string{
		loopingThought(t),
		loopingThought(t),
		sse("", "8443", "", nil, "stop"),
	}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 10, MaxTokens: 512, CtxSize: 8000}, sink)
	defer done()

	if err := a.Ask(context.Background(), "which port"); err != nil {
		t.Fatal(err)
	}
	if n := len(stub.requests); n != 3 {
		t.Errorf("%d requests, want two loops and one answer-only request", n)
	}
	if tools, ok := stub.lastRequest(t)["tools"]; ok {
		t.Errorf("the answer after a second loop still offers tools: %v", tools)
	}
}

// an empty reply after some work is not an answer: the run asks once to go on
func TestAnEmptyReplyAfterToolUseIsAskedToGoOn(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "find_files", []string{`{"pattern":"*.go"}`}, "tool_calls"),
		sse("", "", "", nil, "stop"),
		sse("", "done", "", nil, "stop"),
	}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 512, CtxSize: 8000}, sink)
	defer done()

	if err := a.Ask(context.Background(), "add the field"); err != nil {
		t.Fatal(err)
	}
	if n := len(stub.requests); n != 3 {
		t.Fatalf("%d requests, want the empty reply followed by one more", n)
	}
	if got := sink.content.String(); got != "done" {
		t.Errorf("answer = %q, want the reply after the nudge", got)
	}
	last := stub.lastRequest(t)["messages"].([]any)
	if note := last[len(last)-1].(map[string]any); !strings.Contains(note["content"].(string), "reply was empty") {
		t.Errorf("the model was not told its reply was empty: %v", note)
	}
}
