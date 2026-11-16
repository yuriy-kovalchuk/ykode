package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
	"path/filepath"
	"strings"
	"testing"
)

// The fold is the one stage that reads a result at its full length: it asks about the conversation
// itself, exactly as the server already holds it, so the prompt cache covers the asking and the
// summariser sees what the calls really returned.
func TestAFoldSendsTheConversationItSummarises(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	call := func(id, name, args string) llm.ToolCall {
		return llm.ToolCall{ID: id, Type: "function",
			Function: llm.FunctionCall{Name: name, Arguments: json.RawMessage(args)}}
	}
	stub := &stubServer{scripts: []string{sse("", "Goal: read it\nWhat I learned: there is a line", "", nil, "stop")}}
	a, done := newAgentIn(t, stub, Options{CtxSize: 7000}, &captureSink{}, dir)
	defer done()
	a.add(llm.Message{Role: "user", Content: "read both files"})
	a.add(llm.Message{Role: "assistant", Reasoning: "the reason for that range: the header comes first",
		ToolCalls: []llm.ToolCall{call("c1", "read_file", `{"path":"a.go"}`)}})
	a.add(llm.Message{Role: "tool", ToolCallID: "c1", CallID: "c1", Name: "read_file",
		Content: strings.Repeat("x", 6000) + "\nthe phrase only the result itself holds: quux quux quux\n" + strings.Repeat("y", 2000)})
	a.add(llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call("c2", "read_file", `{"path":"quux"}`)}})
	a.add(llm.Message{Role: "tool", ToolCallID: "c2", CallID: "c2", Name: "read_file", Content: strings.Repeat("z", 6000)})
	a.add(llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call("c3", "read_file", `{"path":"inner.txt"}`)}})
	a.add(llm.Message{Role: "tool", ToolCallID: "c3", CallID: "c3", Name: "read_file", Content: strings.Repeat("w", 4000)})
	before := append([]llm.Message(nil), a.history...)
	start, _ := a.compactCut()

	a.prepareTurn(ctx, 1) // the pressure arrives here, and the fold is the one thing that answers it

	if len(stub.requests) == 0 {
		t.Fatal("no fold request was made")
	}
	fold := stub.requests[0]
	if _, ok := fold["tools"]; !ok {
		t.Error("the fold request carries no tools, so its prefix is not the one the server holds")
	}
	asked := fold["messages"].([]any)
	if len(asked) < start+1 {
		t.Fatalf("the fold was sent %d messages, fewer than the %d older ones it summarises", len(asked)-1, start)
	}
	for i, m := range before[:len(asked)-1] {
		sent := asked[i].(map[string]any)
		if sent["role"] != m.Role || sent["content"] != m.Content {
			t.Fatalf("message %d is not what the server was already given: sent %v, want %q",
				i, sent["content"], m.Content)
		}
	}
	last := asked[len(asked)-1].(map[string]any)
	if last["role"] != "user" {
		t.Errorf("the fold's instruction does not follow the conversation as a user message: %v", last)
	}
	if !strings.Contains(last["content"].(string), "## Goal") {
		t.Error("the fold's instruction does not name the sections the summary is written in")
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
	a, done := newAgentIn(t, stub, Options{MaxTurns: 6, MaxTokens: 64, CtxSize: 40000}, sink, dir)
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
	// the fold is sent the tools every other request sends: a different body means a cold cache
	if tools, ok := stub.requests[4]["tools"].([]any); !ok || len(tools) == 0 {
		t.Error("the summariser request carried no tools, so the server had to re-read the conversation")
	}
}

// a summariser that answers with nothing leaves the conversation alone: nothing is rewritten in
// place, and the notice says the turns stayed as they were
func TestAFailedSummariserChangesNothing(t *testing.T) {
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
	a, done := newAgentIn(t, stub, Options{MaxTurns: 6, MaxTokens: 64, CtxSize: 40000}, sink, dir)
	defer done()

	if err := a.Ask(context.Background(), "read them all"); err != nil {
		t.Fatal(err)
	}
	before := dump(a.history)
	if freed := a.Compact(context.Background()); freed != 0 {
		t.Errorf("a failed fold freed %d tokens, want 0", freed)
	}
	if dump(a.history) != before {
		t.Errorf("a failed fold rewrote the conversation:\n%s", dump(a.history))
	}
	if strings.HasPrefix(a.history[1].Content, FoldMark) {
		t.Error("a summary was inserted even though the summariser answered with nothing")
	}
	if !sink.noticed("could not be summarised") {
		t.Error("the failed fold happened silently")
	}
}

// foldRoom is the window above what the conversation costs at the start: the reads in these tests
// walk it past the pressure mark, and a fold still fits. The room has to hold the summary as well -
// the instruction and compactMaxTokens - over and above what a fold leaves standing, so a fold is
// never refused for being larger than the window it is making room in.
const foldRoom = 5000

func TestARunOutOfRoomFoldsTheOlderTurnsIntoASummary(t *testing.T) {
	dir := t.TempDir()
	names := make([]string, 4)
	for i := range names {
		names[i] = writeLines(t, dir, fmt.Sprintf("file%d.txt", i), 130)
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
	a, done := newAgentIn(t, stub, Options{MaxTurns: 8, MaxTokens: 64}, sink, dir)
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
	a, done := newAgentIn(t, stub, Options{MaxTurns: 6, MaxTokens: 64, CtxSize: 40000}, sink, dir)
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
	a, done := newAgentIn(t, stub, Options{MaxTurns: 6, MaxTokens: 64, CtxSize: 40000}, &captureSink{decision: Allow}, dir)
	defer done()
	if err := a.Ask(context.Background(), ask); err != nil {
		t.Fatal(err)
	}
	// ykode's own notes speak as the user too: one sits between the task and the kept calls
	note := llm.Message{Role: "user", Content: "(the user cancelled this request)"}
	a.history = append(a.history[:4], append([]llm.Message{note}, a.history[4:]...)...)

	a.Compact(context.Background())

	for _, m := range a.history {
		if m.Role == "user" && session.WithoutEnvironment(m.Content) == ask { // the ask, with the note ykode ended it with
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

// summariserRequests counts the requests that ask for a summary of the older turns: the ones whose
// last message is ykode's instruction after the conversation itself.
func summariserRequests(stub *stubServer) int {
	var folds int
	for _, req := range stub.requests {
		messages, _ := req["messages"].([]any)
		if len(messages) == 0 {
			continue
		}
		if last, _ := messages[len(messages)-1].(map[string]any); strings.Contains(fmt.Sprint(last["content"]), "about to leave your context window") {
			folds++
		}
	}
	return folds
}

// a conversation too big for the window to be summarised is refused rather than sent: a fold request
// that the server turns away costs the round-trip and saves nothing
func TestAFoldOfMoreThanTheWindowIsRefused(t *testing.T) {
	a := newForHistory(Options{CtxSize: 1000})
	a.add(llm.Message{Role: "user", Content: "read everything"})
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("c%d", i)
		a.add(llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id, Type: "function",
			Function: llm.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}}}})
		a.add(llm.Message{Role: "tool", ToolCallID: id, CallID: id, Name: "read_file", Content: strings.Repeat("a line of the file that was read\n", 80)})
	}
	before := dump(a.history)

	if freed := a.Compact(context.Background()); freed != 0 {
		t.Errorf("a fold of a conversation larger than the window freed %d tokens, want 0", freed)
	}
	if dump(a.history) != before {
		t.Errorf("the refused fold changed the conversation:\n%s", dump(a.history))
	}
	if len(a.history) < 4 {
		t.Error("the conversation the fold refused to summarise got shorter anyway")
	}
}

// what a front end reads about the cost follows the fold, instead of waiting for the next reply
func TestCompactionRepublishesWhatTheConversationCosts(t *testing.T) {
	dir := t.TempDir()
	names := make([]string, 3)
	for i := range names {
		names[i] = writeLines(t, dir, fmt.Sprintf("file%d.txt", i), 60)
	}
	read := func(i int) string {
		return sse("", "", "read_file", []string{`{"path":"` + names[i] + `"}`}, "tool_calls")
	}
	stub := &stubServer{scripts: []string{
		read(0), read(1), read(2),
		sse("", "all read", "", nil, "stop"),
		sse("", "Goal: read them. Files: three. Decisions: none. Next: answer", "", nil, "stop"),
	}}
	a, done := newAgentIn(t, stub, Options{MaxTurns: 6, MaxTokens: 128, CtxSize: 20000}, &captureSink{decision: Allow}, dir)
	defer done()
	if err := a.Ask(context.Background(), "read them all"); err != nil {
		t.Fatal(err)
	}

	before := a.Usage().EstTok
	freed := a.Compact(context.Background())
	if freed <= 0 {
		t.Fatalf("Compact freed %d tokens with a multi-kilobyte result in the history", freed)
	}
	if after := a.Usage().EstTok; after >= before {
		t.Errorf("the published estimate is still %d after folding %d away; the history now estimates %d",
			after, freed, a.estimateTokens())
	}
}

// the recap the model wrote is shown as it was written: four sections, not one flattened line
func TestTheRecapReachesTheScreen(t *testing.T) {
	dir := t.TempDir()
	names := make([]string, 3)
	for i := range names {
		names[i] = writeLines(t, dir, fmt.Sprintf("file%d.txt", i), 20)
	}
	read := func(i int) string {
		return sse("", "", "read_file", []string{`{"path":"` + names[i] + `"}`}, "tool_calls")
	}
	recap := "Goal: rename the helper\nFiles: file0.txt, file1.txt\nDecided: keep the old name as an alias\nNext: run the tests"
	stub := &stubServer{scripts: []string{
		read(0), read(1), read(2),
		sse("", "all read", "", nil, "stop"),
		sse("", recap, "", nil, "stop"),
	}}
	sink := &captureSink{decision: Allow}
	a, done := newAgentIn(t, stub, Options{MaxTurns: 6, MaxTokens: 64, CtxSize: 40000}, sink, dir)
	defer done()

	if err := a.Ask(context.Background(), "read them all"); err != nil {
		t.Fatal(err)
	}
	if freed := a.Compact(context.Background()); freed <= 0 {
		t.Fatalf("compaction freed %d tokens", freed)
	}
	var got string
	for _, ev := range sink.events {
		if ev.Kind == Summary {
			got = ev.Text
		}
	}
	if !strings.Contains(got, "Goal: rename the helper") || !strings.Contains(got, "\nNext: run the tests") {
		t.Errorf("the recap never reached the screen in its own lines: %q", got)
	}
}

// a conversation too short to fold says what was done instead of looking instant and silent
func TestAFoldThatWasNotNeededSaysSo(t *testing.T) {
	dir := t.TempDir()
	name := writeLines(t, dir, "first.txt", 40)
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"` + name + `"}`}, "tool_calls"),
		sse("", "read it", "", nil, "stop"),
	}}
	sink := &captureSink{decision: Allow}
	a, done := newAgentIn(t, stub, Options{MaxTurns: 4, MaxTokens: 128, CtxSize: 20000}, sink, dir)
	defer done()

	if err := a.Ask(context.Background(), "read it"); err != nil {
		t.Fatal(err)
	}
	a.Compact(context.Background())
	if !sink.noticed("too little older work to fold") {
		t.Errorf("nothing said why there was no recap: %v", sink.kinds())
	}
	for _, ev := range sink.events {
		if ev.Kind == Summary {
			t.Errorf("a conversation that was not folded reported a recap: %q", ev.Text)
		}
	}
}

// a fold belongs to the saved conversation as well: a resume should start from the recap, not
// reload the turns that were folded away and trim them again
func TestAFoldReachesTheSavedConversation(t *testing.T) {
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
		sse("", "Goal: read the files. Files: file0.go. Decisions: none. Next: stop", "", nil, "stop"),
	}}
	saved := session.New(dir, "s20260101-120000", dir)
	a, done := newAgentIn(t, stub, Options{MaxTurns: 6, MaxTokens: 64, CtxSize: 40000, Session: saved}, &captureSink{decision: Allow}, dir)
	defer done()

	if err := a.Ask(context.Background(), "read them all"); err != nil {
		t.Fatal(err)
	}
	if freed := a.Compact(context.Background()); freed <= 0 {
		t.Fatalf("compaction freed %d tokens", freed)
	}

	back, err := session.Load(filepath.Join(dir, "s20260101-120000.jsonl"), dir)
	if err != nil {
		t.Fatal(err)
	}
	var recap bool
	for _, m := range back {
		if strings.Contains(m.Content, "Goal: read the files") {
			recap = true
		}
		if strings.HasPrefix(m.Content, "line 1\nline 2") {
			t.Errorf("what was folded away is still saved: %q", firstChars(m.Content))
		}
	}
	if !recap {
		t.Errorf("the saved conversation holds no recap of the fold: %d messages", len(back))
	}
}

func firstChars(s string) string {
	if len(s) > 40 {
		return s[:40]
	}
	return s
}

// the files the folded turns touched are listed beside the recap, are not summarised again, and
// accumulate across folds and a resume
func TestAFoldNamesTheFilesItFoldedAwayAndTheListSurvivesAResume(t *testing.T) {
	a := &Agent{}
	a.noteFiles([]llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"a.go"}`)}},
			{Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"b.go"}`)}},
			{Function: llm.FunctionCall{Name: "edit_file", Arguments: []byte(`{"path":"b.go"}`)}},
			{Function: llm.FunctionCall{Name: "exec_command", Arguments: []byte(`{"command":"ls"}`)}},
		}},
	})
	blocks := a.fileBlocks()
	if !strings.Contains(blocks, "<read-files>\na.go\n</read-files>") || !strings.Contains(blocks, "<modified-files>\nb.go\n</modified-files>") {
		t.Fatalf("a modified file is listed as read only, or a list is missing:\n%s", blocks)
	}
	recap := llm.Message{Role: "user", Content: FoldMark + "\nGoal: x" + blocks}
	if text, ok := recapOf(recap); !ok || strings.Contains(text, "a.go") || !strings.Contains(text, "Goal: x") {
		t.Errorf("the summary handed back for updating carries the file lists: %q", text)
	}
	b := &Agent{}
	b.restoreFiles([]llm.Message{recap})
	if got := b.fileBlocks(); got != blocks {
		t.Errorf("a resume lost the lists:\n%s\nwant\n%s", got, blocks)
	}
}

// the kept part reaches back until it holds the room asked for, and never starts on a tool result
func TestTheKeptPartReachesBackToTheRoomAskedFor(t *testing.T) {
	var h []llm.Message
	h = append(h, llm.Message{Role: "system", Content: "s"}, llm.Message{Role: "user", Content: "task"})
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("c%d", i)
		h = append(h,
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id, Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"f"}`)}}}},
			llm.Message{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", 4000)})
	}
	a := &Agent{history: h, task: "task"}
	plain, _ := a.compactCut()
	a.opts.KeepRecentTokens = 7000 // more than the two newest calls hold
	wide, _ := a.compactCut()
	if wide == 0 || wide >= plain {
		t.Fatalf("a room of 7000 tokens kept from %d, the two newest calls from %d", wide, plain)
	}
	if h[wide].Role == "tool" {
		t.Errorf("the cut lands on a tool result at %d", wide)
	}
	a.opts.KeepRecentTokens = 1 << 20
	if at, _ := a.compactCut(); at != 0 {
		t.Errorf("a room bigger than the conversation still cut at %d", at)
	}
}

// a reply that is not a summary never replaces the conversation: a model shown a transcript of calls
// may write more calls, and a recap of that would be noise
func TestAReplyThatIsNotASummaryIsNotKept(t *testing.T) {
	for what, reply := range map[string]string{
		"a call written as text": "I'll read the files.\n<tool_call>\n<function=read_file>\n</function>\n</tool_call>",
		"no sections at all":     "ok",
		"nothing":                "",
	} {
		if err := checkSummary(reply); err == nil {
			t.Errorf("%s was accepted as a summary", what)
		}
	}
	if err := checkSummary("## Goal\nfix the bug\n## Next steps\n1. run the tests"); err != nil {
		t.Errorf("a real summary was refused: %v", err)
	}
}

// the newest call and its replies survive a fold word for word: what the model is working from is
// never rewritten, and a fold that lost it would cost the work it is in the middle of
func TestAFoldNeverTouchesTheNewestCall(t *testing.T) {
	dir := t.TempDir()
	names := make([]string, 4)
	for i := range names {
		names[i] = writeLines(t, dir, fmt.Sprintf("file%d.txt", i), 130)
	}
	read := func(i int) string {
		return sse("", "", "read_file", []string{`{"path":"` + names[i] + `"}`}, "tool_calls")
	}
	stub := &stubServer{scripts: []string{
		read(0), read(1), read(2), read(3),
		sse("", "Goal: read them. Files: four. Decisions: none. Next: answer", "", nil, "stop"),
	}}
	a, done := newAgentIn(t, stub, Options{MaxTurns: 8, MaxTokens: 64, CtxSize: 40000}, &captureSink{}, dir)
	defer done()
	if err := a.Ask(context.Background(), "read them all"); err != nil {
		t.Fatal(err)
	}

	start := len(a.history)
	for i := len(a.history) - 1; i >= 0; i-- {
		if m := a.history[i]; m.Role == "assistant" && len(m.ToolCalls) > 0 {
			start = i
			break
		}
	}
	newest := dump(a.history[start:])
	if freed := a.Compact(context.Background()); freed <= 0 {
		t.Fatal("nothing was folded")
	}
	end := len(a.history)
	for i := len(a.history) - 1; i >= 0; i-- {
		if m := a.history[i]; m.Role == "assistant" && len(m.ToolCalls) > 0 {
			end = i
			break
		}
	}
	if got := dump(a.history[end:]); got != newest {
		t.Errorf("the newest call was changed by the fold:\n%s\nwant\n%s", got, newest)
	}
}

// a model that answers the fold with a tool call is asked once more with tools refused, and a second
// call is refused: a summary is the only thing a fold may write
func TestAFoldAnsweredWithACallIsRetriedWithoutTools(t *testing.T) {
	dir := t.TempDir()
	names := make([]string, 3)
	for i := range names {
		names[i] = writeLines(t, dir, fmt.Sprintf("file%d.txt", i), 130)
	}
	read := func(i int) string {
		return sse("", "", "read_file", []string{`{"path":"` + names[i] + `"}`}, "tool_calls")
	}
	call := sse("", "", "read_file", []string{`{"path":"file0.txt"}`}, "tool_calls")
	stub := &stubServer{scripts: []string{
		read(0), read(1), read(2),
		sse("", "all read", "", nil, "stop"),
		call, call,
	}}
	a, done := newAgentIn(t, stub, Options{MaxTurns: 6, MaxTokens: 64, CtxSize: 40000}, &captureSink{}, dir)
	defer done()
	if err := a.Ask(context.Background(), "read them all"); err != nil {
		t.Fatal(err)
	}
	before := dump(a.history)
	if freed := a.Compact(context.Background()); freed != 0 {
		t.Errorf("a fold that was answered with calls freed %d tokens, want 0", freed)
	}
	if dump(a.history) != before {
		t.Errorf("a fold that was answered with calls rewrote the conversation:\n%s", dump(a.history))
	}
	folds := stub.requests[4:]
	if len(folds) != 2 {
		t.Fatalf("%d fold requests, want 2", len(folds))
	}
	if folds[1]["tool_choice"] != "none" {
		t.Errorf("the second fold asked with tool_choice %v, want the tools refused", folds[1]["tool_choice"])
	}
}

// a long run folds more than once, and the next fold waits for new work: asking again every turn
// would pay for a request and a cold prompt cache each time
func TestTheNextFoldWaitsForNewCalls(t *testing.T) {
	a := newForHistory(Options{})
	if !a.foldOverdue() {
		t.Fatal("a conversation that was never folded is not due for a fold")
	}
	a.compactedAt = 0
	history := []llm.Message{{Role: "system", Content: SystemPrompt}, {Role: "user", Content: "go"}}
	for i := 0; i < foldGap-1; i++ {
		id := fmt.Sprintf("c%d", i)
		history = append(history, llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id,
			Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"a.go"}`)}}}})
	}
	a.history = history
	if a.foldOverdue() {
		t.Errorf("%d new calls were not enough to ask again, want %d", callBlocks(history)-a.compactedAt, foldGap)
	}
	a.history = append(history, llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "last",
		Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"a.go"}`)}}}})
	if !a.foldOverdue() {
		t.Error("the next fold never became due")
	}
}

// the recap an earlier fold left is sent to the next one as part of the conversation, so the second
// summary updates what the first kept instead of starting over
func TestTheNextFoldCarriesTheEarlierRecap(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "## Goal\nthe work went on", "", nil, "stop")}}
	a, done := newAgentIn(t, stub, Options{CtxSize: 7000}, &captureSink{}, t.TempDir())
	defer done()
	recap := llm.Message{Role: "user", Content: FoldMark + "\n## Goal\nthe work so far"}
	a.add(recap)
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("c%d", i)
		a.add(llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id, Type: "function",
			Function: llm.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}}}})
		a.add(llm.Message{Role: "tool", ToolCallID: id, CallID: id, Name: "read_file", Content: strings.Repeat("x", 3000)})
	}

	if freed := a.Compact(context.Background()); freed <= 0 {
		t.Fatal("the second fold freed nothing")
	}
	messages, _ := stub.requests[0]["messages"].([]any)
	var carried bool
	for _, raw := range messages {
		if msg, _ := raw.(map[string]any); fmt.Sprint(msg["content"]) == recap.Content {
			carried = true
		}
	}
	if !carried {
		t.Error("the earlier recap was left out of the request, so the new summary starts from scratch")
	}
}

// a small window has few calls and each one is big, so insisting on keeping the newest two can leave
// nothing to fold at all: the fold keeps one newest call instead, because a fold that frees something
// is always better than a deletion
func TestASmallWindowFoldsRatherThanDeleteTheOlderCall(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "## Goal\nthe first read is summarized here\n", "", nil, "stop")}}
	sink := &captureSink{}
	a, done := newAgentIn(t, stub, Options{CtxSize: 8192, KeepRecentTokens: 8192 / 5}, sink, t.TempDir())
	defer done()

	const ask = "read both files"
	a.task = ask
	a.add(llm.Message{Role: "user", Content: ask})
	for i, path := range []string{"a.txt", "b.txt"} { // two calls: the newest two are the whole run
		id := fmt.Sprintf("c%d", i)
		a.add(llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id, Type: "function",
			Function: llm.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"` + path + `"}`)}}}})
		a.add(llm.Message{Role: "tool", ToolCallID: id, CallID: id, Name: "read_file", Content: strings.Repeat("x", 9000)})
	}

	if a.Compact(context.Background()) <= 0 {
		t.Fatal("nothing was folded, so the drop layer has to delete a real result")
	}
	var folds int
	for _, ev := range sink.events {
		if ev.Kind == Summary {
			folds++
		}
	}
	if folds == 0 {
		t.Fatal("the fold ran without a recap")
	}
	if kept := callBlocks(a.history); kept < 1 {
		t.Errorf("the fold left %d whole calls, want the newest one at least", kept)
	}
}
