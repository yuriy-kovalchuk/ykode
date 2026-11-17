package agent

import (
	"context"
	"strings"
	"testing"
)

func TestARunIsAStreamOfTypedEvents(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("pondering", "", "read_file", []string{`{"path":"seeded.txt"}`}, "tool_calls"),
		sse("", "done", "", nil, "stop"),
	}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 512}, sink)
	defer done()

	if err := a.Ask(context.Background(), "list the files"); err != nil {
		t.Fatal(err)
	}
	want := []EventKind{TurnStart, Thinking, ToolDraft, TurnEnd, ToolStart, ToolEnd, TurnStart, Answer, TurnEnd}
	got := sink.kinds()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", kindsText(got), kindsText(want))
	}
	for i, k := range want {
		if got[i] != k {
			t.Fatalf("event %d = %v, want %v (%v)", i, got[i], k, kindsText(got))
		}
	}
	if draft := sink.events[2]; draft.Tool != "read_file" || draft.Text != `{"path":"seeded.txt"}` {
		t.Fatalf("tool_draft event = %+v, want the call being written", draft)
	}
	if sink.events[5].Tool != "read_file" || !strings.Contains(sink.events[5].Text, "one line") {
		t.Fatalf("tool_end event = %+v, want the tool name and its result", sink.events[5])
	}
	for _, ev := range sink.events {
		if ev.Kind != Answer && ev.Turn == 0 {
			t.Fatalf("event %v carries no turn number", ev.Kind)
		}
	}
}

func TestAFailingToolIsReportedAsAnEvent(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"missing.go"}`}, "tool_calls"),
		sse("", "nothing there", "", nil, "stop"),
	}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 512}, sink)
	defer done()

	if err := a.Ask(context.Background(), "read missing.go"); err != nil {
		t.Fatal(err)
	}
	var failed Event
	for _, ev := range sink.events {
		if ev.Kind == ToolFail {
			failed = ev
		}
	}
	if failed.Tool != "read_file" || failed.Text == "" {
		t.Fatalf("no tool_fail event in %v", kindsText(sink.kinds()))
	}
}

func kindsText(kinds []EventKind) string {
	parts := make([]string, len(kinds))
	for i, k := range kinds {
		parts[i] = k.String()
	}
	return strings.Join(parts, " ")
}

// a front end draws a call from the events, so they carry it whole, uncut and unbroken
func TestToolEventsCarryTheWholeCallAndResult(t *testing.T) {
	content := strings.Repeat("hello world\\n", 40) // far past 200 bytes of arguments
	args := `{"path":"a.txt","content":"` + content + `"}`
	stub := &stubServer{scripts: []string{
		sse("", "", "write_file", []string{args}, "tool_calls"),
		sse("", "done", "", nil, "stop"),
	}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 512, AssumeYes: true}, sink)
	defer done()
	if err := a.Ask(context.Background(), "write it"); err != nil {
		t.Fatal(err)
	}
	for _, ev := range sink.events {
		if ev.Kind != ToolStart && ev.Kind != ToolEnd {
			continue
		}
		if ev.Args != args {
			t.Fatalf("%v carries %d bytes of arguments, want the call's %d", ev.Kind, len(ev.Args), len(args))
		}
	}
}

// a call written as text never reaches the answer: it is stripped and the model is asked once again
func TestACallWrittenAsTextIsStrippedAndTheModelAskedOnce(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "It is in ops.go.<tool_call><function=read_file></function></tool_call>", "", nil, "stop"),
		sse("", "It is in ops.go.", "", nil, "stop"),
	}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 4, MaxTokens: 64}, sink)
	defer done()
	if err := a.Ask(context.Background(), "where is it?"); err != nil {
		t.Fatal(err)
	}
	for _, m := range a.history {
		if m.Role == "assistant" && hasTextCall(m.Content) {
			t.Errorf("the markup stayed in the conversation: %q", m.Content)
		}
	}
	var told bool
	for _, ev := range sink.events {
		told = told || (ev.Kind == Notice && strings.Contains(ev.Text, "tool call as text"))
	}
	if !told || len(stub.requests) != 2 {
		t.Errorf("nudged %v, %d requests; want one nudge and a second request", told, len(stub.requests))
	}
}
