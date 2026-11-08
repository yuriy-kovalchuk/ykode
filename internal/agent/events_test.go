package agent

import (
	"context"
	"strings"
	"testing"
)

func TestARunIsAStreamOfTypedEvents(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("pondering", "", "find_files", []string{`{"pattern":"*","depth":1}`}, "tool_calls"),
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
	if draft := sink.events[2]; draft.Tool != "find_files" || draft.Text != `{"pattern":"*","depth":1}` {
		t.Fatalf("tool_draft event = %+v, want the call being written", draft)
	}
	if sink.events[5].Tool != "find_files" || sink.events[5].Text == "" {
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
