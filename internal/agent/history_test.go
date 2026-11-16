package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// newForHistory is an agent with no tools and no server, for walking a conversation through what the
// window rules do to it.
func newForHistory(opts Options) *Agent {
	return New(nil, nil, "test-model", opts, nullSink{})
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
	a.makeRoom()

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

// the drop keeps what it must: a fold's recap, the ask being answered and the newest call, and it
// leaves the estimate under the ceiling
func TestADropKeepsTheRecapTheTaskAndTheNewestCall(t *testing.T) {
	history := []llm.Message{{Role: "system", Content: SystemPrompt},
		{Role: "user", Content: FoldMark + "\nGoal: the work so far"},
		{Role: "user", Content: "read them all"}}
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("c%d", i)
		history = append(history,
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id, Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"f.go"}`)}}}},
			llm.Message{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", 400)})
	}
	a := newForHistory(Options{CtxSize: 900})
	a.task = "read them all"
	a.history = history
	a.makeRoom()

	var recap, task bool
	for _, m := range a.history {
		if _, ok := recapOf(m); ok {
			recap = true
		}
		if m.Role == "user" && m.Content == a.task {
			task = true
		}
	}
	if !recap {
		t.Error("a fold's recap was dropped: the model lost the memory of everything before it")
	}
	if !task {
		t.Error("the ask being answered was dropped")
	}
	if blocks := callBlocks(a.history); blocks == 0 {
		t.Error("the newest call was dropped; the model still needs what it just read")
	}
	if !paired(a.history) {
		t.Errorf("the drop broke call pairing:\n%s", dump(a.history))
	}
	if got := a.estimateTokens(); got > a.ceiling() {
		t.Errorf("after dropping, %d tokens still, over the ceiling of %d", got, a.ceiling())
	}
}

func TestTheDropNeverSendsAnOrphanToolReply(t *testing.T) {
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
		a.makeRoom()
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
	a.makeRoom()
	a.sanitizeHistory()

	for _, m := range a.history {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 && len(m.ToolCalls[0].Function.Arguments) == 0 {
			t.Fatal("a tool call was altered")
		}
	}
	if !paired(a.history) {
		t.Fatalf("dropping in an interrupted turn left a call without replies:\n%s", dump(a.history))
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

// the screen asks what the model is being sent, so a run that came back can show what it came back with
func TestTheConversationIsReadableBack(t *testing.T) {
	a, done := newAgentIn(t, &stubServer{}, Options{}, &captureSink{}, t.TempDir())
	defer done()
	a.add(llm.Message{Role: "user", Content: "an ask from before"})

	saved := a.Conversation()
	if saved[0].Role != "system" {
		t.Errorf("the conversation starts with %q, not the system prompt", saved[0].Role)
	}
	if last := saved[len(saved)-1]; last.Role != "user" || last.Content != "an ask from before" {
		t.Errorf("the ask is not at the end: %+v", last)
	}
	saved[len(saved)-1].Content = "changed somewhere else"
	if got := a.Conversation()[len(saved)-1].Content; got != "an ask from before" {
		t.Errorf("reading the conversation hands out the conversation itself: %q", got)
	}
}

// The server says how many tokens the conversation it just read was. Guessing the whole conversation
// again from a byte ratio throws that number away every turn, and the error grows with the conversation.
func TestTheWindowIsMeasuredFromWhatTheServerCounted(t *testing.T) {
	dir := t.TempDir()
	a, done := newAgentIn(t, &stubServer{}, Options{CtxSize: 4000}, &captureSink{}, dir)
	defer done()
	a.add(llm.Message{Role: "user", Content: strings.Repeat("a", 500)})

	a.meter.observe(Stats{PromptTokens: 1234}, a.sendingBytes())
	if got := a.estimateTokens(); got != 1234 {
		t.Errorf("the server counted 1234 tokens for this conversation and the run estimates %d", got)
	}

	a.add(llm.Message{Role: "user", Content: strings.Repeat("b", 300)})
	moved := int(float64(300) / max(a.meter.bytesPerTok, minBytesPerTok))
	if got := a.estimateTokens(); got != 1234+moved {
		t.Errorf("after 300 more bytes the estimate is %d, want the counted 1234 plus %d for what moved", got, moved)
	}
}

// the marks exist so a summary, not a deletion, is what pays for room: a long run at a real window
// size must never lose a whole call that a fold could still have written down
func TestALongRunPaysForRoomWithFoldsNotDeletions(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "## Goal\nthe reading goes on\n", "", nil, "stop")}}
	sink := &captureSink{}
	a, done := newAgentIn(t, stub, Options{CtxSize: 32768, KeepRecentTokens: 32768 / 5}, sink, t.TempDir())
	defer done()

	const ask = "read the file until the window fills"
	a.task = ask
	a.add(llm.Message{Role: "user", Content: ask})
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("c%d", i)
		a.add(llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id, Type: "function",
			Function: llm.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"big.txt"}`)}}}})
		a.add(llm.Message{Role: "tool", ToolCallID: id, CallID: id, Name: "read_file", Content: strings.Repeat("r", 8000)})
		a.prepareTurn(context.Background(), i+1)
	}

	var folds int
	var dropped []string
	for _, ev := range sink.events {
		switch {
		case ev.Kind == Summary:
			folds++
		case ev.Kind == Notice && strings.HasPrefix(ev.Text, "dropped"):
			dropped = append(dropped, ev.Text)
		}
	}
	if folds == 0 {
		t.Fatal("a run 20 calls long never folded")
	}
	if len(dropped) > 0 {
		t.Errorf("a fold could still have written this down, so nothing should have been deleted: %v", dropped)
	}
	if a.estimateTokens() > a.ceiling() {
		t.Errorf("after 20 calls the conversation is %d tokens, over the ceiling of %d", a.estimateTokens(), a.ceiling())
	}
}

// a dropped call is only survivable if the model is told what went: it can redo that call on purpose
// instead of finding a hole in its memory and guessing at it
func TestTheDropNamesWhatItTookAway(t *testing.T) {
	sink := &captureSink{}
	history := []llm.Message{{Role: "system", Content: SystemPrompt}, {Role: "user", Content: "read them all"}}
	for _, call := range []struct{ id, name, args string }{
		{"c1", "read_file", `{"path":"src/parser.go"}`},
		{"c2", "exec_command", `{"command":"go test ./..."}`},
		{"c3", "read_file", `{"path":"keep.go"}`},
	} {
		history = append(history,
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: call.id,
				Function: llm.FunctionCall{Name: call.name, Arguments: []byte(call.args)}}}},
			llm.Message{Role: "tool", ToolCallID: call.id, Name: call.name, Content: strings.Repeat("x", 400)})
	}
	a := New(nil, nil, "test-model", Options{CtxSize: (len(SystemPrompt) + 900) / 4}, sink)
	a.task = "read them all"
	a.history = history

	a.makeRoom()

	var said string
	for _, ev := range sink.events {
		if ev.Kind == Notice && strings.HasPrefix(ev.Text, "dropped") {
			said = ev.Text
		}
	}
	if said == "" {
		t.Fatal("the drop happened without a word about it")
	}
	for _, want := range []string{"read_file src/parser.go", "exec_command"} {
		if !strings.Contains(said, want) {
			t.Errorf("%q is missing from what the drop reported:\n%s", want, said)
		}
	}
}

// the estimate runs inside the loop that drops messages, so the tool list is described to the agent
// once and its size kept: rebuilding five descriptions per message dropped is work nobody reads
func TestTheEstimateNeverRedescribesTheToolList(t *testing.T) {
	described := 0
	a := New(nil, tools.NewRegistry(&countingTool{described: &described}), "m", Options{}, nullSink{})
	a.history = []llm.Message{{Role: "system", Content: SystemPrompt}, {Role: "user", Content: "go"}}

	a.estimateTokens()
	a.estimateTokens()

	if a.wireSpecBytes() == 0 {
		t.Error("the tool list counts for nothing in the estimate")
	}
	if described != 1 {
		t.Errorf("the tool list was described %d times for two estimates, want once", described)
	}
}

type countingTool struct{ described *int }

func (c *countingTool) Name() string       { return "read_file" }
func (c *countingTool) Class() tools.Class { return tools.ClassReads }
func (c *countingTool) Description() string {
	*c.described++
	return "counts how often it was described"
}
func (c *countingTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
}
func (c *countingTool) NeedsConfirm() bool                              { return false }
func (c *countingTool) Targets(json.RawMessage) ([]tools.Target, error) { return nil, nil }
func (c *countingTool) Run(_ context.Context, _ json.RawMessage) (string, error) {
	return "read", nil
}

// a drop makes room for more than the next result: dropping one message per round moved the start of
// the prompt every round, and the server read the whole conversation again each time
func TestADropLeavesRoomForTheNextRounds(t *testing.T) {
	history := []llm.Message{{Role: "system", Content: SystemPrompt}, {Role: "user", Content: "read them all"}}
	call := func(i int) []llm.Message {
		id := fmt.Sprintf("c%d", i)
		return []llm.Message{
			{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id, Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"f.go"}`)}}}},
			{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", 2000)}}
	}
	for i := 0; i < 60; i++ {
		history = append(history, call(i)...)
	}
	a := newForHistory(Options{CtxSize: 32000})
	a.task = "read them all"
	a.history = history
	a.makeRoom()

	a.history = append(a.history, call(60)...)
	before := len(a.history)
	a.makeRoom()
	if len(a.history) != before {
		t.Errorf("the round after a drop dropped again: %d messages became %d", before, len(a.history))
	}
}
