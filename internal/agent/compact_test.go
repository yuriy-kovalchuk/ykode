package agent

import (
	"context"
	"fmt"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCompactShortensTheRestAndSaysHowMuch(t *testing.T) {
	dir := t.TempDir()
	name := writeLines(t, dir, "first.txt", 60)
	stub := &stubServer{scripts: []string{
		sse("", "", "search_text", []string{`{"pattern":"line ","path":"` + name + `"}`}, "tool_calls"),
		sse("", "done", "", nil, "stop"),
	}}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 4, MaxTokens: 128, CtxSize: 20000}, &captureSink{decision: Allow}, dir)
	defer done()

	if err := a.Ask(context.Background(), "read it"); err != nil {
		t.Fatal(err)
	}
	before := a.estimateTokens()
	if freed := a.Compact(context.Background()); freed <= 0 {
		t.Fatalf("Compact freed %d tokens with a multi-kilobyte result in the history", freed)
	}
	if after := a.estimateTokens(); after >= before {
		t.Errorf("the conversation is %d tokens after Compact and %d before", after, before)
	}
	if _, err := os.ReadFile(filepath.Join(dir, "saved", "r1.txt")); err != nil {
		t.Errorf("Compact dropped the result without keeping it: %v", err)
	}
}

func TestCompactReplacesOlderTurnsWithTheModelSummary(t *testing.T) {
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
	sink := &captureSink{decision: Allow}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 6, MaxTokens: 64, CtxSize: 40000}, sink, dir)
	defer done()

	if err := a.Ask(context.Background(), "read them all"); err != nil {
		t.Fatal(err)
	}
	if freed := a.Compact(context.Background()); freed <= 0 {
		t.Fatalf("compaction freed %d tokens with three results in the history", freed)
	}
	summary := a.history[1]
	if summary.Role != "user" || !strings.Contains(summary.Content, "Goal: rename the helper") {
		t.Fatalf("the summary did not land where the model will see it: %+v", summary)
	}
	if !strings.Contains(summary.Content, "compact") {
		t.Errorf("the summary is not marked as something ykode wrote: %q", summary.Content)
	}
	if blocks := callBlocks(a.history); blocks != compactKeep {
		t.Errorf("%d tool calls left, want the newest %d kept", blocks, compactKeep)
	}
	if !paired(a.history) {
		t.Fatalf("compaction broke call pairing:\n%s", dump(a.history))
	}
	if len(stub.requests) < 5 {
		t.Fatal("no summariser request was made")
	}
	if tools, ok := stub.requests[4]["tools"].([]any); ok && len(tools) > 0 {
		t.Errorf("the summariser was offered %d tools; it only writes text", len(tools))
	}
}

func TestCompactStillFreesRoomWhenTheSummariserFails(t *testing.T) {
	dir := t.TempDir()
	names := make([]string, 3)
	for i := range names {
		names[i] = writeLines(t, dir, fmt.Sprintf("big%d.txt", i), 40)
	}
	read := func(i int) string {
		return sse("", "", "read_file", []string{`{"path":"` + names[i] + `"}`}, "tool_calls")
	}
	stub := &stubServer{scripts: []string{
		read(0), read(1), read(2),
		sse("", "all read", "", nil, "stop"),
		sse("", "", "", nil, "stop"), // the summariser answers with nothing
	}}
	sink := &captureSink{decision: Allow}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 6, MaxTokens: 64, CtxSize: 40000}, sink, dir)
	defer done()

	if err := a.Ask(context.Background(), "read them all"); err != nil {
		t.Fatal(err)
	}
	if freed := a.Compact(context.Background()); freed <= 0 {
		t.Fatalf("compaction freed %d tokens after the summariser failed", freed)
	}
	var shortened bool
	for _, m := range a.history {
		if m.Role == "tool" && strings.HasPrefix(m.Content, shrinkMark) {
			shortened = true
		}
	}
	if !shortened {
		t.Error("the fallback left the tool results untouched")
	}
	if strings.HasPrefix(a.history[1].Content, compactMessage) {
		t.Error("a summary was inserted even though the summariser answered with nothing")
	}
	if !sink.noticed("shortening tool results") {
		t.Error("the fallback happened silently")
	}
}

// foldRoom is the window above the fixed prompt cost: three 40-line reads overflow it, one fold fits.
const foldRoom = 2000

func TestARunOutOfRoomFoldsTheOlderTurnsIntoASummary(t *testing.T) {
	dir := t.TempDir()
	names := make([]string, 4)
	for i := range names {
		names[i] = writeLines(t, dir, fmt.Sprintf("file%d.txt", i), 40)
	}
	read := func(i int) string {
		return sse("", "", "read_file", []string{`{"path":"` + names[i] + `"}`}, "tool_calls")
	}
	stub := &stubServer{scripts: []string{
		read(0), read(1), read(2), read(3),
		sse("", "Goal: read them all. Files: file0-3. Decisions: none. Next: answer", "", nil, "stop"),
		sse("", "done", "", nil, "stop"),
	}}
	sink := &captureSink{decision: Allow}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 8, MaxTokens: 64}, sink, dir)
	defer done()
	a.opts.CtxSize = a.estimateTokens() + foldRoom

	if err := a.Ask(context.Background(), "read them all then answer"); err != nil {
		t.Fatal(err)
	}
	if summariserRequests(stub) == 0 {
		t.Error("no tool-free summariser request was made while the conversation was out of room")
	}
	if !sink.noticed("summarise the older turns") {
		t.Error("the run paid for a summary without telling the user")
	}
	if !paired(a.history) {
		t.Errorf("auto-compaction left the history unpaired:\n%s", dump(a.history))
	}
}

func TestCompactionWithoutASummaryStillKeepsTheNewestWriteWhole(t *testing.T) {
	dir := t.TempDir()
	body := strings.Repeat("text the surviving turns still need in full ", 60)
	write := func(i int) string {
		args := `{"path":"out` + strconv.Itoa(i) + `.txt","content":"` + body + `"}`
		return sse("", "", "write_file", []string{args}, "tool_calls")
	}
	stub := &stubServer{scripts: []string{
		write(0), write(1), write(2),
		sse("", "all written", "", nil, "stop"),
		sse("", "", "", nil, "stop"), // the summariser answers with nothing, so the fallback runs
	}}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 6, MaxTokens: 64, CtxSize: 20000}, &captureSink{decision: Allow}, dir)
	defer done()

	if err := a.Ask(context.Background(), "write them"); err != nil {
		t.Fatal(err)
	}
	if freed := a.Compact(context.Background()); freed <= 0 {
		t.Fatalf("compaction freed %d tokens after the summariser failed", freed)
	}
	var kept, reduced int
	for _, m := range a.history {
		for _, c := range m.ToolCalls {
			if strings.Contains(string(c.Function.Arguments), body) {
				kept++
			} else {
				reduced++
			}
		}
	}
	if kept != compactKeep {
		t.Errorf("%d of the newest writes kept their text, want %d", kept, compactKeep)
	}
	if reduced == 0 {
		t.Error("the older writes are still carrying the whole file they wrote")
	}
}

// The summary is for what happened, not for the words of the job in hand: a model that has to
// work from a paraphrase of the request starts answering a question nobody asked.
func TestCompactionKeepsTheWordingOfTheCurrentTask(t *testing.T) {
	dir := t.TempDir()
	names := make([]string, 3)
	for i := range names {
		names[i] = writeLines(t, dir, fmt.Sprintf("file%d.txt", i), 20)
	}
	read := func(i int) string {
		return sse("", "", "read_file", []string{`{"path":"` + names[i] + `"}`}, "tool_calls")
	}
	ask := "rename helper1_0 to keepThisName and do not touch the other helpers"
	stub := &stubServer{scripts: []string{
		read(0), read(1), read(2),
		sse("", "all read", "", nil, "stop"),
		sse("", "Goal: rename a helper. Files: three. Decisions: none. Next: edit", "", nil, "stop"),
	}}
	sink := &captureSink{decision: Allow}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 6, MaxTokens: 64, CtxSize: 40000}, sink, dir)
	defer done()

	if err := a.Ask(context.Background(), ask); err != nil {
		t.Fatal(err)
	}
	if freed := a.Compact(context.Background()); freed <= 0 {
		t.Fatalf("compaction freed %d tokens", freed)
	}
	var kept bool
	for _, m := range a.history {
		if m.Role == "user" && strings.Contains(m.Content, ask) {
			kept = true
		}
	}
	if !kept {
		t.Errorf("the task's own words were paraphrased away; history now reads:\n%s", historyText(a))
	}
}

func TestCompactionKeepsTheTaskNotANoteYkodeAddedAfterIt(t *testing.T) {
	dir := t.TempDir()
	names := make([]string, 3)
	for i := range names {
		names[i] = writeLines(t, dir, fmt.Sprintf("file%d.txt", i), 20)
	}
	read := func(i int) string {
		return sse("", "", "read_file", []string{`{"path":"` + names[i] + `"}`}, "tool_calls")
	}
	ask := "rename helper1_0 to keepThisName and do not touch the other helpers"
	stub := &stubServer{scripts: []string{
		read(0), read(1), read(2),
		sse("", "all read", "", nil, "stop"),
		sse("", "Goal: rename a helper. Files: three. Decisions: none. Next: edit", "", nil, "stop"),
	}}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 6, MaxTokens: 64, CtxSize: 40000}, &captureSink{decision: Allow}, dir)
	defer done()
	if err := a.Ask(context.Background(), ask); err != nil {
		t.Fatal(err)
	}
	// ykode's own notes speak as the user too: one sits between the task and the kept calls
	note := llm.Message{Role: "user", Content: verifyNote}
	a.history = append(a.history[:4], append([]llm.Message{note}, a.history[4:]...)...)

	a.Compact(context.Background())

	for _, m := range a.history {
		if m.Role == "user" && m.Content == ask {
			return
		}
	}
	t.Errorf("a note was kept instead of the task; history now reads:\n%s", historyText(a))
}

func TestAFoldWaitsForSeveralNewCallsBeforeTheNext(t *testing.T) {
	calls := func(n int) []llm.Message {
		var history []llm.Message
		for range n {
			history = append(history, llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c"}}})
		}
		return history
	}
	a := &Agent{compactedAt: compactKeep}
	a.history = calls(compactKeep + 2)
	if a.foldOverdue() {
		t.Error("two calls after a fold already asked for another: a summary request and a cold cache every two calls")
	}
	a.history = calls(compactKeep + foldGap)
	if !a.foldOverdue() {
		t.Errorf("%d new calls since the fold did not ask for another", foldGap)
	}
}

// historyText is a readable dump of the conversation, for failure messages.
func historyText(a *Agent) string {
	var b strings.Builder
	for _, m := range a.history {
		fmt.Fprintf(&b, "  %s: %s\n", m.Role, oneLine(m.Content, 80))
	}
	return b.String()
}

// One fold per session is not enough: a long run empties the window again, and the second time
// there is nothing left to shorten, so the turns have to be folded again.
func TestTheRunFoldsTheHistoryAgainWhenTheCallsPileBackUp(t *testing.T) {
	dir := t.TempDir()
	names := make([]string, 3+foldGap+1)
	for i := range names {
		names[i] = writeLines(t, dir, fmt.Sprintf("file%d.txt", i), 40)
	}
	read := func(i int) string {
		return sse("", "", "read_file", []string{`{"path":"` + names[i] + `"}`}, "tool_calls")
	}
	scripts := []string{read(0), read(1), read(2),
		sse("", "Goal: read them. Files: three. Decisions: none. Next: keep reading", "", nil, "stop")}
	for i := 3; i < 3+foldGap; i++ {
		scripts = append(scripts, read(i))
	}
	scripts = append(scripts,
		sse("", "Goal: read them. Files: nine. Decisions: none. Next: keep reading", "", nil, "stop"),
		read(len(names)-1),
		sse("", "all read", "", nil, "stop"))
	stub := &stubServer{scripts: scripts}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: len(names) + 1, MaxTokens: 64}, &captureSink{}, dir)
	defer done()
	a.opts.CtxSize = a.estimateTokens() + foldRoom

	if err := a.Ask(context.Background(), "read every file and say how many lines each has"); err != nil {
		t.Fatal(err)
	}
	if folds := summariserRequests(stub); folds < 2 {
		t.Errorf("the window emptied twice but the model was asked to summarise %d times", folds)
	}
	if !lastFoldSawTheEarlierSummary(stub) {
		t.Error("the second fold started from scratch and lost what the first one kept")
	}
}

// summariserRequests counts the requests that ask for a summary of the older turns.
func summariserRequests(stub *stubServer) int {
	var folds int
	for _, req := range stub.requests {
		messages, _ := req["messages"].([]any)
		if len(messages) == 0 {
			continue
		}
		if first, _ := messages[0].(map[string]any); first["content"] == compactPrompt {
			folds++
		}
	}
	return folds
}

// lastFoldSawTheEarlierSummary says whether the summariser was given the summary from an earlier
// fold, so the second one does not lose what the first one kept.
func lastFoldSawTheEarlierSummary(stub *stubServer) bool {
	seen := false
	for _, req := range stub.requests {
		messages, _ := req["messages"].([]any)
		if len(messages) == 0 {
			continue
		}
		if first, _ := messages[0].(map[string]any); first["content"] != compactPrompt {
			continue
		}
		seen = false
		for _, raw := range messages[1:] {
			if msg, _ := raw.(map[string]any); strings.Contains(fmt.Sprint(msg["content"]), compactMessage) {
				seen = true
			}
		}
	}
	return seen
}

func TestTheSummariserIsNeverHandedMoreThanTheWindowHolds(t *testing.T) {
	a := newForHistory(Options{CtxSize: 1000})
	var messages []llm.Message
	for i := range 300 {
		messages = append(messages, llm.Message{Role: "user", Content: fmt.Sprintf("turn %d says something worth a line", i)})
	}

	text := a.transcriptOf(messages)

	if limit := int(float64(a.opts.CtxSize*2/5) * a.meter.bytesPerTok); len(text) > limit {
		t.Errorf("transcript is %d bytes, over the %d a 40%% share of the window holds", len(text), limit)
	}
	if !strings.Contains(text, "turn 0 ") || !strings.Contains(text, "turn 299 ") {
		t.Error("the cut lost the start (an earlier summary) or the end (the latest work)")
	}
}
