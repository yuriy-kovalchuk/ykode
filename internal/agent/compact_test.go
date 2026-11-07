package agent

import (
	"context"
	"fmt"
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
	for _, ev := range sink.events {
		if ev.Kind == Notice && strings.Contains(ev.Text, "shortening tool results") {
			return
		}
	}
	t.Error("the fallback happened silently")
}

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
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 8, MaxTokens: 64, CtxSize: 2600}, sink, dir)
	defer done()

	if err := a.Ask(context.Background(), "read them all then answer"); err != nil {
		t.Fatal(err)
	}
	var summarised, notices bool
	for _, req := range stub.requests {
		if _, hasTools := req["tools"]; hasTools {
			continue
		}
		if first, ok := req["messages"].([]any)[0].(map[string]any); ok && strings.Contains(first["content"].(string), "Summarise the earlier part") {
			summarised = true
		}
	}
	for _, ev := range sink.events {
		if ev.Kind == Notice && strings.Contains(ev.Text, "summarise the older turns") {
			notices = true
		}
	}
	if !summarised {
		t.Error("no tool-free summariser request was made while the conversation was out of room")
	}
	if !notices {
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
	names := make([]string, 8)
	for i := range names {
		names[i] = writeLines(t, dir, fmt.Sprintf("file%d.txt", i), 40)
	}
	read := func(i int) string {
		return sse("", "", "read_file", []string{`{"path":"` + names[i] + `"}`}, "tool_calls")
	}
	stub := &stubServer{scripts: []string{
		read(0), read(1), read(2),
		sse("", "Goal: read them. Files: three. Decisions: none. Next: keep reading", "", nil, "stop"),
		read(3), read(4),
		sse("", "Goal: read them. Files: five. Decisions: none. Next: keep reading", "", nil, "stop"),
		read(5), read(6), read(7),
		sse("", "all eight read", "", nil, "stop"),
	}}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 9, MaxTokens: 64, CtxSize: 3000}, &captureSink{}, dir)
	defer done()

	if err := a.Ask(context.Background(), "read all eight files and say how many lines each has"); err != nil {
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
