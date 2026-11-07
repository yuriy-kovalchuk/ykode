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
