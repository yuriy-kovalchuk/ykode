package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// newStoringAgent wires the built-in tools plus a result store, so shrinking and recall can be tested together.
func newStoringAgent(t *testing.T, stub *stubServer, opts Options, sink Sink, dir string) (*Agent, func()) {
	t.Helper()
	server := httptest.NewServer(stub)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	store := tools.NewResultStore(filepath.Join(dir, "saved"))
	registry := tools.BuiltIn(root, dir, tools.Options{Store: store, MaxResultBytes: 4000})
	a := New(llm.NewClient(server.URL+"/v1", ""), registry, "test-model", opts, sink)
	a.opts.Store = store
	return a, func() { server.Close(); root.Close() }
}

// writeLines makes a text file of n lines, each long enough that a read of it is worth shrinking.
func writeLines(t *testing.T, dir, name string, n int) string {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString("line " + strconv.Itoa(i) + " of " + name + " carrying enough text to matter\n")
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

// toolContents lists the role:"tool" replies the server was sent, oldest first.
func toolContents(t *testing.T, req map[string]any) []string {
	t.Helper()
	list, ok := req["messages"].([]any)
	if !ok {
		t.Fatal("the request carries no message list")
	}
	var out []string
	for _, m := range list {
		if msg := m.(map[string]any); msg["role"] == "tool" {
			out = append(out, msg["content"].(string))
		}
	}
	return out
}

func TestAnOldResultIsSummarisedAndStillReachable(t *testing.T) {
	dir := t.TempDir()
	// five different files: a repeat of one window now comes back as a pointer, not a fresh result
	names := make([]string, 5)
	for i := range names {
		names[i] = writeLines(t, dir, fmt.Sprintf("file%d.txt", i), 20)
	}
	// a search is cheap and read-only: a shortened one says to run it again, and keeps no copy that
	// a later edit would make wrong
	search := func(i int) string {
		return sse("", "", "search_text", []string{`{"pattern":"line ","path":"` + names[i] + `"}`}, "tool_calls")
	}
	stub := &stubServer{scripts: []string{search(0), search(1), search(2), search(3), search(4), sse("", "done", "", nil, "stop")}}
	sink := &captureSink{decision: Allow}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 8, MaxTokens: 128, CtxSize: 4300}, sink, dir)
	defer done()

	if err := a.Ask(context.Background(), "search five files"); err != nil {
		t.Fatal(err)
	}
	if !sink.noticed("shortened older tool output") {
		t.Error("results were shortened without telling the user")
	}
	contents := toolContents(t, stub.requests[len(stub.requests)-1])
	if len(contents) < 3 {
		t.Fatalf("%d tool results in the last request, want at least three", len(contents))
	}
	summary := contents[0]
	if !strings.HasPrefix(summary, shrinkMark) {
		t.Fatalf("the oldest result is still whole:\n%s", brief(summary))
	}
	if !strings.Contains(summary, "run the same search_text again") {
		t.Fatalf("the summary does not say how to get the text back:\n%s", summary)
	}
	if !strings.Contains(summary, "search_text") || !strings.Contains(summary, "it began:") {
		t.Fatalf("the summary does not say what the result was:\n%s", summary)
	}
	for _, recent := range contents[len(contents)-keepResults:] {
		if strings.HasPrefix(recent, shrinkMark) {
			t.Errorf("a result the model is still working with was shrunk:\n%s", brief(recent))
		}
	}
	if _, err := os.ReadFile(filepath.Join(dir, "saved", "r1.txt")); err == nil {
		t.Error("a search result was stored: it goes stale the moment a file changes")
	}
}

func brief(s string) string {
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

func TestTheToolListStaysFixedWhileResultsAreShortened(t *testing.T) {
	dir := t.TempDir()
	names := make([]string, 5)
	for i := range names {
		names[i] = writeLines(t, dir, fmt.Sprintf("file%d.txt", i), 20)
	}
	read := func(i int) string {
		return sse("", "", "read_file", []string{`{"path":"` + names[i] + `"}`}, "tool_calls")
	}
	stub := &stubServer{scripts: []string{read(0), read(1), read(2), read(3), read(4), sse("", "done", "", nil, "stop")}}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 8, MaxTokens: 128, CtxSize: 4000}, &captureSink{decision: Allow}, dir)
	defer done()

	if err := a.Ask(context.Background(), "read them all"); err != nil {
		t.Fatal(err)
	}
	if !toolNames(stub.requests[0])["read_file"] {
		t.Fatal("the first request lost a tool it should always have")
	}
	first := stub.requests[0]["tools"]
	for i, next := range stub.requests[1:] {
		if next["tools"] == nil {
			continue // the compaction summariser is asked without tools, on purpose
		}
		if !reflect.DeepEqual(first, next["tools"]) {
			t.Errorf("request %d changed the tool list mid-session", i+1)
		}
	}
}

func TestRecallIsAbsentWhenNothingCanBeKept(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	a := New(nil, tools.BuiltIn(root, dir, tools.Options{}), "m", Options{}, nil)
	for _, spec := range a.Specs() {
		if spec.Name == "recall" {
			t.Error("recall was offered with no store to keep results in")
		}
	}
}

func toolNames(req map[string]any) map[string]bool {
	names := map[string]bool{}
	tools, _ := req["tools"].([]any)
	for _, t := range tools {
		fn := t.(map[string]any)["function"].(map[string]any)
		names[fn["name"].(string)] = true
	}
	return names
}

// callArguments lists the tool calls the server was asked for, oldest first.
func callArguments(t *testing.T, req map[string]any) []llm.FunctionCall {
	t.Helper()
	raw, err := json.Marshal(req["messages"])
	if err != nil {
		t.Fatal(err)
	}
	var msgs []llm.Message
	if err := json.Unmarshal(raw, &msgs); err != nil {
		t.Fatal(err)
	}
	var out []llm.FunctionCall
	for _, m := range msgs {
		for _, c := range m.ToolCalls {
			out = append(out, c.Function)
		}
	}
	return out
}

func TestAnOldWriteKeepsItsIntentNotItsText(t *testing.T) {
	dir := t.TempDir()
	body := strings.Repeat("a line of the file that is not worth resending every turn ", 60)
	write := func(i int) string {
		args := `{"path":"out` + strconv.Itoa(i) + `.txt","content":"` + body + `"}`
		return sse("", "", "write_file", []string{args}, "tool_calls")
	}
	stub := &stubServer{scripts: []string{write(0), write(1), write(2), write(3), sse("", "done", "", nil, "stop")}}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 8, MaxTokens: 128, CtxSize: 7500}, &captureSink{decision: Allow}, dir)
	defer done()

	if err := a.Ask(context.Background(), "write them"); err != nil {
		t.Fatal(err)
	}
	calls := callArguments(t, stub.requests[len(stub.requests)-1])
	if len(calls) < 4 {
		t.Fatalf("%d calls in the last request, want at least four", len(calls))
	}
	var dropped, whole int
	for _, c := range calls {
		var shrunk struct {
			Ykode string `json:"_ykode"`
		}
		if json.Unmarshal(c.Arguments, &shrunk) == nil && shrunk.Ykode != "" {
			dropped++
			if !strings.Contains(shrunk.Ykode, "wrote ") || !strings.Contains(shrunk.Ykode, "KB") ||
				!strings.Contains(shrunk.Ykode, ".txt") {
				t.Errorf("the summary does not say what was written where: %s", shrunk.Ykode)
			}
			if !regexp.MustCompile(`r[0-9]+ was the reply`).MatchString(shrunk.Ykode) {
				t.Errorf("the summary does not point at the result: %s", shrunk.Ykode)
			}
			continue
		}
		whole++
		if !strings.Contains(string(c.Arguments), body) {
			t.Errorf("a call left whole lost its text: %s", brief(string(c.Arguments)))
		}
	}
	if dropped == 0 {
		t.Error("every write is still carrying the whole file it wrote")
	}
	if whole < keepResults {
		t.Errorf("only %d of the newest writes kept their text, want %d", whole, keepResults)
	}
	if _, err := os.Stat(filepath.Join(dir, "out0.txt")); err != nil {
		t.Errorf("the file itself went missing: %v", err)
	}
	if !paired(a.history) {
		t.Errorf("shrinking arguments broke the pairing:\n%s", dump(a.history))
	}
}

func TestTheNewestWritesAndTheFailedOnesKeepTheirText(t *testing.T) {
	a := New(nil, nil, "m", Options{}, nil)
	body := strings.Repeat("text the model has already paid to send once ", 40)
	write := `{"path":"%s","content":"` + body + `"}`
	edit := `{"path":"%s","edits":[{"old_text":"one","new_text":"` + body + `"}]}`
	for n, spec := range []struct{ tool, args, reply string }{
		{"write_file", fmt.Sprintf(write, "a.txt"), "wrote it"},
		{"write_file", fmt.Sprintf(write, "b.txt"), "error: path is required"}, // a retry may need this one
		{"edit_file", fmt.Sprintf(edit, "c.txt"), "changed c.txt"},
		{"write_file", fmt.Sprintf(write, "d.txt"), "wrote it"},
		{"write_file", fmt.Sprintf(write, "e.txt"), "wrote it"},
	} {
		id := fmt.Sprintf("call%d", n+1)
		a.history = append(a.history,
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{
				ID:       id,
				Type:     "function",
				Function: llm.FunctionCall{Name: spec.tool, Arguments: json.RawMessage(spec.args)},
			}}},
			llm.Message{Role: "tool", Name: spec.tool, ToolCallID: id, CallID: fmt.Sprintf("r%d", n+1), Content: spec.reply},
		)
	}

	if freed := a.shrinkOldEdits(keepResults); freed <= 0 {
		t.Fatalf("shrinkOldEdits freed %d bytes", freed)
	}
	var got []string
	for _, m := range a.history {
		for _, c := range m.ToolCalls {
			got = append(got, string(c.Function.Arguments))
		}
	}
	if len(got) != 5 {
		t.Fatalf("%d calls in the history, want 5", len(got))
	}
	// a.txt worked and is old enough to reduce; b.txt failed, so it stays whole
	if !strings.Contains(got[0], `"_ykode"`) || !strings.Contains(got[0], "a.txt") || !strings.Contains(got[0], "r1") {
		t.Errorf("the oldest working write is still whole: %s", brief(got[0]))
	}
	if !strings.Contains(got[1], body) {
		t.Errorf("the failed write lost the text a retry would need: %s", brief(got[1]))
	}
	if !strings.Contains(got[2], "changed c.txt") || !strings.Contains(got[2], "1 replacement") ||
		!strings.Contains(got[2], "r3") {
		t.Errorf("an edit is summarised like a write, or not at all: %s", brief(got[2]))
	}
	for _, args := range got[len(got)-keepResults:] {
		if !strings.Contains(args, body) {
			t.Errorf("a write the model may still act on was reduced: %s", brief(args))
		}
	}
	for _, args := range got {
		if !json.Valid([]byte(args)) {
			t.Errorf("the history now carries arguments a server cannot parse: %s", brief(args))
		}
	}
	if again := a.shrinkOldEdits(keepResults); again != 0 {
		t.Errorf("shrinking a second time freed %d more bytes: it ran over text it had already replaced", again)
	}
	if !paired(a.history) {
		t.Errorf("reducing arguments broke the pairing:\n%s", dump(a.history))
	}
}

func TestTwoCallsWithTheSameServerIDPointAtTheirOwnReply(t *testing.T) {
	a := New(nil, nil, "m", Options{}, nil)
	body := strings.Repeat("enough text that shrinking is worth doing at all ", 40)
	// llama.cpp repeats one id across calls, so matching by id would point every summary at r1
	for n := range 3 {
		a.history = append(a.history,
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{
				ID:   "call",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "write_file",
					Arguments: json.RawMessage(`{"path":"f` + strconv.Itoa(n) + `.txt","content":"` + body + `"}`),
				},
			}}},
			llm.Message{Role: "tool", Name: "write_file", ToolCallID: "call", CallID: fmt.Sprintf("r%d", n+1), Content: "wrote it"},
		)
	}
	if freed := a.shrinkOldEdits(1); freed <= 0 {
		t.Fatalf("shrinkOldEdits freed %d bytes", freed)
	}
	var ids []string
	for _, m := range a.history {
		for _, c := range m.ToolCalls {
			var shrunk struct {
				Ykode string `json:"_ykode"`
			}
			if json.Unmarshal(c.Function.Arguments, &shrunk) == nil && shrunk.Ykode != "" {
				ids = append(ids, shrunk.Ykode)
			}
		}
	}
	if len(ids) != 2 {
		t.Fatalf("%d calls were reduced, want 2: %v", len(ids), ids)
	}
	for want, got := range ids {
		if !strings.Contains(got, fmt.Sprintf("r%d was the reply", want+1)) {
			t.Errorf("the %d oldest summary does not name its own reply: %s", want+1, got)
		}
	}
}

func TestAStaleFileReadIsNotStoredForRecall(t *testing.T) {
	dir := t.TempDir()
	names := make([]string, 5)
	for i := range names {
		names[i] = writeLines(t, dir, fmt.Sprintf("read%d.txt", i), 20)
	}
	read := func(i int) string {
		return sse("", "", "read_file", []string{`{"path":"` + names[i] + `"}`}, "tool_calls")
	}
	stub := &stubServer{scripts: []string{read(0), read(1), read(2), read(3), read(4), sse("", "done", "", nil, "stop")}}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 8, MaxTokens: 128, CtxSize: 4000}, &captureSink{decision: Allow}, dir)
	defer done()

	if err := a.Ask(context.Background(), "read five files"); err != nil {
		t.Fatal(err)
	}
	summary := toolContents(t, stub.requests[len(stub.requests)-1])[0]
	if !strings.HasPrefix(summary, shrinkMark) {
		t.Fatalf("the oldest read is still whole:\n%s", brief(summary))
	}
	if strings.Contains(summary, "recall") {
		t.Errorf("a shortened file read offers recall, which could hand back text a later edit changed:\n%s", brief(summary))
	}
	if !strings.Contains(summary, "read the range you need again") {
		t.Errorf("the shortened read does not say how to get the text back:\n%s", brief(summary))
	}
	if _, err := os.ReadFile(filepath.Join(dir, "saved", "r1.txt")); !os.IsNotExist(err) {
		t.Errorf("a file read was stored for recall: %v", err)
	}
}

func TestAReReadAfterShorteningBringsTheTextBack(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	dir := t.TempDir()
	name := writeLines(t, dir, "big.txt", 40)
	a, done := newStoringAgent(t, stub, Options{CtxSize: 8000}, &captureSink{}, dir)
	defer done()

	reader, ok := a.tools.Get("read_file")
	if !ok {
		t.Fatal("the registry has no read_file")
	}
	args := json.RawMessage(`{"path":"` + name + `"}`)
	first, err := reader.Run(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	a.history = append(a.history,
		llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID:       "call1",
			Type:     "function",
			Function: llm.FunctionCall{Name: "read_file", Arguments: args},
		}}},
		llm.Message{Role: "tool", Name: "read_file", ToolCallID: "call1", CallID: "r1", Content: first},
	)
	if freed := a.shrinkOldResults(0); freed == 0 {
		t.Fatal("the read was not shortened, so there was nothing to test")
	}

	again, err := reader.Run(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(again, "unchanged since you read") {
		t.Errorf("the text was shortened out of the conversation and the tool still refused to send it: %q", again)
	}
}

// an old edit is summarised with what it actually wrote, in either argument shape
func TestAnEditSummaryCountsTheTextItWrote(t *testing.T) {
	body := strings.Repeat("x", 2048)
	for name, args := range map[string]string{
		"one change": `{"path":"c.go","old_text":"one","new_text":"` + body + `"}`,
		"edits":      `{"path":"c.go","edits":[{"old_text":"one","new_text":"` + body + `"}]}`,
	} {
		raw, ok := editSummary("edit_file", llm.FunctionCall{Name: "edit_file", Arguments: json.RawMessage(args)}, "r3")
		if !ok {
			t.Fatalf("%s: no summary", name)
		}
		if got := string(raw); !strings.Contains(got, "1 replacement(s), 2.0 KB of new text") {
			t.Errorf("%s: summary = %s, want one replacement of 2.0 KB", name, got)
		}
	}
}
