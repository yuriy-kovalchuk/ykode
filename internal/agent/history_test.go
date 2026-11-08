package agent

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// twoReads is a conversation with two read results of the given sizes, the oldest first.
func twoReads(older, newer int) []llm.Message {
	return []llm.Message{
		{Role: "system", Content: SystemPrompt},
		{Role: "user", Content: "fix the bug"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"a.go"}`)}}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: strings.Repeat("o", older)},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c2", Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"b.go"}`)}}}},
		{Role: "tool", ToolCallID: "c2", Name: "read_file", Content: strings.Repeat("n", newer)},
		{Role: "assistant", Content: "done"},
	}
}

// historyFixture has two large results, of which only the oldest may go.
func historyFixture() []llm.Message { return twoReads(2000, 2000) }

func newForHistory(opts Options) *Agent {
	return New(nil, nil, "test-model", opts, nullSink{})
}

// sizedFixture has one huge old result and one small recent one, so trimming has an obvious first victim.
func sizedFixture() []llm.Message { return twoReads(4000, 800) }

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
	heavy := func() []llm.Message { return twoReads(6000, 6000) }
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

func TestTrimNeverBlanksAResultTheModelHasNotSeen(t *testing.T) {
	unseen := func() []llm.Message { return sizedFixture()[:6] } // ends on a tool reply: no answer yet
	a := newForHistory(Options{CtxSize: windowBelow(t, unseen)}) // trimming the old result is enough
	a.history = unseen()

	a.trimHistory()

	for _, m := range a.history {
		if m.ToolCallID == "c2" && m.Content == trimMarker {
			t.Error("the reply the model is about to read was blanked before it ever saw it")
		}
	}
}

// a command is slow to run again, so its trimmed output is kept in a file to grep
func TestATrimmedCommandOutputIsKeptInAFile(t *testing.T) {
	store := tools.NewResultStore(t.TempDir())
	a := newForHistory(Options{CtxSize: windowBelow(t, sizedFixture), Store: store})
	a.history = sizedFixture()
	a.history[3].Name, a.history[3].CallID = "exec_command", "r1"
	full := a.history[3].Content

	a.trimHistory()

	if !strings.Contains(a.history[3].Content, "r1.txt") || !strings.Contains(a.history[3].Content, "sed -n") {
		t.Fatalf("the trimmed result names no way back: %q", a.history[3].Content)
	}
	if saved, err := store.Load("r1"); err != nil || saved != full {
		t.Errorf("the trimmed text was not kept: %v", err)
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

	withTools := New(nil, tools.NewRegistry(tools.NewReadFile(root, dir)), "m", Options{}, nullSink{})
	without := New(nil, nil, "m", Options{}, nullSink{})
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

// paired says the history is something a server accepts: every tool reply has the call it answers,
// and every call has the replies it is owed.
func paired(history []llm.Message) bool {
	owed := map[string]bool{}
	for _, m := range history {
		if m.Role == "assistant" {
			for _, call := range m.ToolCalls {
				owed[call.ID] = false
			}
		}
		if m.Role == "tool" {
			answered, ok := owed[m.ToolCallID]
			if !ok || answered {
				return false
			}
			owed[m.ToolCallID] = true
		}
	}
	for _, answered := range owed {
		if !answered {
			return false
		}
	}
	return true
}

func TestASmallWindowDropsTheOldestToolCallWhole(t *testing.T) {
	history := []llm.Message{{Role: "system", Content: SystemPrompt}, {Role: "user", Content: "read them all"}}
	for i, id := range []string{"c1", "c2", "c3"} {
		history = append(history,
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id, Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"f.go"}`)}}}},
			llm.Message{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", 300+i)},
		)
	}
	a := newForHistory(Options{CtxSize: (len(SystemPrompt) + 900) / 4})
	a.history = history
	a.trimHistory()

	if !paired(a.history) {
		t.Fatalf("a tool reply was left without its call:\n%s", dump(a.history))
	}
	if blocks := callBlocks(a.history); blocks >= 3 {
		t.Errorf("%d tool calls left, want the oldest dropped", blocks)
	}
	var oldest, newest bool
	for _, m := range a.history {
		if m.Role == "tool" && m.ToolCallID == "c1" {
			oldest = true
		}
		if m.Role == "tool" && m.ToolCallID == "c3" {
			newest = true
		}
	}
	if oldest {
		t.Error("the oldest tool reply is still in the conversation the server sees")
	}
	if !newest {
		t.Error("the newest tool reply was dropped; the model still needs what it just read")
	}
}

func TestTrimNeverSendsAnOrphanToolReply(t *testing.T) {
	for _, ctx := range []int{200, 400, 800, 1600, 6000} {
		history := []llm.Message{{Role: "system", Content: SystemPrompt}, {Role: "user", Content: "go"}}
		for i := range 4 {
			id := string(rune('a' + i))
			history = append(history,
				llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id, Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"f.go"}`)}}}},
				llm.Message{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("y", 500)},
			)
		}
		a := newForHistory(Options{CtxSize: ctx})
		a.history = history
		a.trimHistory()
		if !paired(a.history) {
			t.Errorf("ctx %d left an orphan tool reply", ctx)
		}
	}
}

func dump(history []llm.Message) string {
	var b strings.Builder
	for _, m := range history {
		fmt.Fprintf(&b, "%s %s\n", m.Role, m.ToolCallID)
	}
	return b.String()
}

func TestACallStillOwedItsRepliesIsNeverDropped(t *testing.T) {
	history := []llm.Message{
		{Role: "system", Content: SystemPrompt},
		{Role: "user", Content: "go"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"a.go"}`)}}}},
		{Role: "assistant", Content: strings.Repeat("thinking ", 60)},
		{Role: "user", Content: "keep going"},
	}
	a := newForHistory(Options{CtxSize: 120})
	a.history = history
	a.sanitizeHistory()
	a.trimHistory()
	a.sanitizeHistory()

	for _, m := range a.history {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 && len(m.ToolCalls[0].Function.Arguments) == 0 {
			t.Fatal("a tool call was altered")
		}
	}
	if !paired(a.history) {
		t.Fatalf("trimming an interrupted turn left a call without replies:\n%s", dump(a.history))
	}
}

func TestACallThatNeverRanIsAnsweredBeforeTheServerSeesIt(t *testing.T) {
	a := newForHistory(Options{})
	a.history = []llm.Message{
		{Role: "system", Content: SystemPrompt},
		{Role: "user", Content: "go"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "c1", Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"a.go"}`)}},
			{ID: "c2", Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"b.go"}`)}},
		}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "package a"},
	}
	a.sanitizeHistory()

	if !paired(a.history) {
		t.Fatalf("c2 reached the request without a reply:\n%s", dump(a.history))
	}
	last := a.history[len(a.history)-1]
	if last.ToolCallID != "c2" || !strings.Contains(last.Content, "interrupted") {
		t.Errorf("the invented reply is %q for %q", last.Content, last.ToolCallID)
	}
	a.sanitizeHistory()
	if len(a.history) != 5 {
		t.Errorf("sanitizing twice invented another reply: %d messages, want 5", len(a.history))
	}
}
