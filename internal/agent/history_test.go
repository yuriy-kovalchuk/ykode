package agent

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// historyFixture is a conversation with two large tool results, of which only the oldest may go.
func historyFixture() []llm.Message {
	big := strings.Repeat("x", 2000)
	return []llm.Message{
		{Role: "system", Content: SystemPrompt},
		{Role: "user", Content: "fix the bug"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"a.go"}`)}}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: big},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c2", Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"b.go"}`)}}}},
		{Role: "tool", ToolCallID: "c2", Name: "read_file", Content: big},
		{Role: "assistant", Content: "done"},
	}
}

func newForHistory(opts Options) *Agent {
	return New(nil, nil, "test-model", opts, quietSink{}, io.Discard)
}

// sizedFixture has one huge old result and one small recent one, so trimming has an obvious first victim.
func sizedFixture() []llm.Message {
	return []llm.Message{
		{Role: "system", Content: SystemPrompt},
		{Role: "user", Content: "fix the bug"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"a.go"}`)}}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: strings.Repeat("o", 4000)},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c2", Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"b.go"}`)}}}},
		{Role: "tool", ToolCallID: "c2", Name: "read_file", Content: strings.Repeat("n", 800)},
		{Role: "assistant", Content: "done"},
	}
}

// windowBelow returns a context window that the untrimmed history has passed 3/4 of,
// and that one trim of history[3] already brings under half.
func windowBelow(t *testing.T, history func() []llm.Message) int {
	t.Helper()
	trimmed := newForHistory(Options{})
	trimmed.history = history()
	trimmed.history[3].Content = trimMarker
	ctx := 2*trimmed.estimateTokens() + 200

	full := newForHistory(Options{})
	full.history = history()
	if got := full.estimateTokens(); got <= ctx*3/4 {
		t.Fatalf("fixture estimate %d has not passed 3/4 of %d: the trim would never run", got, ctx)
	}
	return ctx
}

func TestTrimHistorySacrificesTheOldestBigResultFirst(t *testing.T) {
	history := sizedFixture()
	a := newForHistory(Options{CtxSize: windowBelow(t, sizedFixture)})
	a.history = history

	a.trimHistory()

	if a.history[0].Content != SystemPrompt {
		t.Fatal("the system prompt must never be trimmed")
	}
	if a.history[3].Content != trimMarker {
		t.Fatalf("oldest tool result was not trimmed: %.60q", a.history[3].Content)
	}
	if a.history[5].Content == trimMarker {
		t.Fatal("the newest tool result was trimmed while an older one still had room")
	}
	for _, m := range a.history {
		if m.Role == "tool" && (m.ToolCallID == "" || m.Name == "") {
			t.Fatal("trimming must not remove the pairing fields")
		}
	}
	for _, m := range a.history {
		for _, c := range m.ToolCalls {
			if !hasToolAnswer(a.history, c.ID) {
				t.Fatalf("tool call %s lost its answer", c.ID)
			}
		}
	}
}

// Two equally huge results: one trim lands under 3/4 but still above half, so the trim must keep going.
func TestTrimOncePastTheThresholdKeepsGoingToHalfTheWindow(t *testing.T) {
	heavy := func() []llm.Message {
		return []llm.Message{
			{Role: "system", Content: SystemPrompt},
			{Role: "user", Content: "fix the bug"},
			{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"a.go"}`)}}}},
			{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: strings.Repeat("o", 6000)},
			{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c2", Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"b.go"}`)}}}},
			{Role: "tool", ToolCallID: "c2", Name: "read_file", Content: strings.Repeat("n", 6000)},
			{Role: "assistant", Content: "done"},
		}
	}
	oneTrim := newForHistory(Options{})
	oneTrim.history = heavy()
	oneTrim.history[3].Content = trimMarker
	ctx := oneTrim.estimateTokens()*4/3 + 1 // one trim lands just under the 3/4 line

	a := newForHistory(Options{CtxSize: ctx})
	a.history = heavy()
	a.trimHistory()

	if trimmed := trimCount(a.history); trimmed < 2 {
		t.Errorf("trimmed %d results, want both: stopping at the threshold breaks the prompt cache again next turn", trimmed)
	}
	if got := a.estimateTokens(); got > ctx/2 {
		t.Errorf("estimate %d is above half of the %d window the trim was aiming at", got, ctx)
	}
}

func TestTrimHistoryDoesNothingBelowTheThreshold(t *testing.T) {
	history := sizedFixture()
	a := newForHistory(Options{CtxSize: windowBelow(t, sizedFixture) * 4})
	a.history = history

	a.trimHistory()

	if trimmed := trimCount(a.history); trimmed != 0 {
		t.Fatalf("trimmed %d results below the 3/4 threshold; it must not touch a roomy conversation", trimmed)
	}
}

func TestTrimHistoryLeavesSmallResultsAndNoContextAlone(t *testing.T) {
	// this context forces the trimmer to walk the history; short results must still be left alone
	a := newForHistory(Options{CtxSize: 400})
	a.history = historyFixture()
	a.history[3].Content = "short answer"
	a.history[5].Content = "short answer"

	a.trimHistory()

	if trimmed := trimCount(a.history); trimmed != 0 {
		t.Fatalf("trimmed %d results, want none: a 400 byte floor protects them", trimmed)
	}

	off := newForHistory(Options{})
	off.history = historyFixture()
	off.trimHistory()
	if trimmed := trimCount(off.history); trimmed != 0 {
		t.Fatalf("trimming ran without --ctx (%d trimmed); it must be off", trimmed)
	}
}

func TestHistoryBytesCountsToolArguments(t *testing.T) {
	one := []llm.Message{{Role: "user", Content: strings.Repeat("a", 30)}}
	bigger := []llm.Message{{Role: "assistant", ToolCalls: []llm.ToolCall{
		{ID: "c1", Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(strings.Repeat("b", 30))}},
	}}}
	if got, want := historyBytes(one), 30; got != want {
		t.Fatalf("plain message = %d bytes, want %d", got, want)
	}
	if historyBytes(bigger) <= historyBytes(one) {
		t.Fatal("tool call bytes must count toward the estimate")
	}
}

func TestTheEstimateIncludesTheToolSpecs(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	withTools := New(nil, tools.NewRegistry(tools.NewReadFile(root, dir)), "m", Options{}, quietSink{}, io.Discard)
	without := New(nil, nil, "m", Options{}, quietSink{}, io.Discard)
	if withTools.estimateTokens() <= without.estimateTokens() {
		t.Fatal("tool specs must count toward the context estimate")
	}
}

func hasToolAnswer(history []llm.Message, id string) bool {
	for _, m := range history {
		if m.Role == "tool" && m.ToolCallID == id {
			return true
		}
	}
	return false
}

func trimCount(history []llm.Message) int {
	n := 0
	for _, m := range history {
		if m.Role == "tool" && m.Content == trimMarker {
			n++
		}
	}
	return n
}
