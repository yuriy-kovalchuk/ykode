package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// the few words a front end puts beside its working prompt, so a long round-trip is not a mystery
func TestTrimmingSaysWhichItDid(t *testing.T) {
	a := newForHistory(Options{CtxSize: windowBelow(t, sizedFixture)})
	a.history = sizedFixture()
	if made := a.RoomMade(); made != "" {
		t.Fatalf("nothing has run, yet it already says %q", made)
	}
	a.trimHistory()
	if made := a.RoomMade(); made != "trimmed 1 tool result" {
		t.Errorf("RoomMade = %q, want the one result that was blanked", made)
	}
}

// losing a whole call is worth knowing about more than losing the middle of a result
func TestDroppingWholeCallsNamesTheDrops(t *testing.T) {
	history := []llm.Message{{Role: "system", Content: SystemPrompt}, {Role: "user", Content: "read them all"}}
	for _, id := range []string{"c1", "c2", "c3"} {
		history = append(history,
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id, Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"f.go"}`)}}}},
			llm.Message{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", 300)})
	}
	a := newForHistory(Options{CtxSize: (len(SystemPrompt) + 900) / 4})
	a.history = history

	a.trimHistory()

	if made := a.RoomMade(); !strings.HasPrefix(made, "dropped ") {
		t.Errorf("RoomMade = %q, want the whole calls that went", made)
	}
}

// a fold says what it folded, and the ask after it starts without that saying
func TestAFoldSaysWhatItFoldedUntilTheNextAsk(t *testing.T) {
	dir := t.TempDir()
	names := make([]string, 3)
	for i := range names {
		names[i] = writeLines(t, dir, fmt.Sprintf("file%d.txt", i), 20)
	}
	read := func(i int) string {
		return sse("", "", "read_file", []string{`{"path":"` + names[i] + `"}`}, "tool_calls")
	}
	stub := &stubServer{scripts: []string{
		read(0), read(1), read(2),
		sse("", "all read", "", nil, "stop"),
		sse("", "Goal: rename the helper. Files: file0.go. Decisions: none. Next: run the tests", "", nil, "stop"),
	}}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 6, MaxTokens: 64, CtxSize: 40000}, &captureSink{decision: Allow}, dir)
	defer done()

	if err := a.Ask(context.Background(), "read them all"); err != nil {
		t.Fatal(err)
	}
	if freed := a.Compact(context.Background()); freed <= 0 {
		t.Fatalf("compaction freed %d tokens with three results in the history", freed)
	}
	if made := a.RoomMade(); !strings.HasPrefix(made, "folded ") {
		t.Errorf("RoomMade = %q, want the turns the fold took away", made)
	}

	if err := a.Ask(context.Background(), "and now?"); err != nil {
		t.Fatal(err)
	}
	if made := a.RoomMade(); made != "" {
		t.Errorf("the new ask still reports what the last one did: %q", made)
	}
}
