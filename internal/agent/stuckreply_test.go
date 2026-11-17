package agent

import (
	"context"
	"encoding/json"
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

// a looping answer is stopped and ends the ask; what it repeated is not kept
func TestALoopingAnswerIsStopped(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", strings.Repeat(loopParagraph, stuckLineLimit+5), "", nil, "stop")}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 4, MaxTokens: 512, CtxSize: 8000}, sink)
	defer done()

	if err := a.Ask(context.Background(), "which port"); err != nil {
		t.Fatal(err)
	}
	if !sink.noticed("kept repeating the same lines") || len(stub.requests) != 1 {
		t.Errorf("%d requests; want one, stopped, and the user told", len(stub.requests))
	}
	for _, m := range a.history {
		if strings.Contains(m.Content, "Let me re-read the file") {
			t.Error("the looping text was kept in the history")
		}
	}
}

// thinking that drafts code repeats lines honestly, and its budget already ends it: it is not stopped
func TestRepeatedLinesInThinkingAreNotALoop(t *testing.T) {
	draft := strings.Repeat("if err != nil { t.Fatalf(\"read: %v\", err) }\n", stuckLineLimit+5)
	stub := &stubServer{scripts: []string{sse(draft, "8443", "", nil, "stop")}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 4, MaxTokens: 512, CtxSize: 8000}, sink)
	defer done()

	if err := a.Ask(context.Background(), "which port"); err != nil {
		t.Fatal(err)
	}
	if got := sink.content.String(); got != "8443" || sink.noticed("kept repeating") {
		t.Errorf("answer = %q; the thinking was taken for a loop", got)
	}
}

// an empty reply ends the ask and says so; nothing is sent to make the model go on
func TestAnEmptyReplyEndsTheAsk(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"a.go"}`}, "tool_calls"),
		sse("", "", "", nil, "stop"),
	}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 512, CtxSize: 8000}, sink)
	defer done()

	if err := a.Ask(context.Background(), "add the field"); err != nil {
		t.Fatal(err)
	}
	if n := len(stub.requests); n != 2 || !sink.noticed("returned no text") {
		t.Fatalf("%d requests, want the empty reply reported and the ask ended", n)
	}
}

// a reply that ends after its thinking is asked once to go on, and the ask ends when it answers
func TestAReplyThatOnlyThoughtIsAskedOnceToGoOn(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("Let me grep the lines.", "", "", nil, "stop"),
		sse("", "done", "", nil, "stop"),
	}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 512, CtxSize: 8000}, sink)
	defer done()

	if err := a.Ask(context.Background(), "add the field"); err != nil {
		t.Fatal(err)
	}
	if n := len(stub.requests); n != 2 || !sink.noticed("stopped after thinking") {
		t.Fatalf("%d requests, want the thinking-only reply followed by one request to go on", n)
	}
	if sent, _ := json.Marshal(stub.requests[1]); !strings.Contains(string(sent), "ended after your thinking") {
		t.Errorf("the second request does not say why it was sent")
	}
	if sink.noticed("returned no text") {
		t.Errorf("an ask that got its answer reported an empty reply")
	}
}

// the request to go on is made once per ask: a second reply that only thinks ends it
func TestAReplyThatOnlyThinksTwiceEndsTheAsk(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("thinking", "", "", nil, "stop"),
		sse("still thinking", "", "", nil, "stop"),
	}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 512, CtxSize: 8000}, sink)
	defer done()

	if err := a.Ask(context.Background(), "add the field"); err != nil {
		t.Fatal(err)
	}
	if n := len(stub.requests); n != 2 || !sink.noticed("returned no text") {
		t.Fatalf("%d requests, want one request to go on and then the ask ended", n)
	}
}
