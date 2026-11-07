package agent

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/audit"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

func newAuditedAgent(t *testing.T, stub *stubServer, opts Options, sink Sink) (*Agent, func(), string, *audit.Logger) {
	t.Helper()
	server := httptest.NewServer(stub)
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	logger := audit.New(filepath.Join(dir, "audit"), "stest")
	registry := tools.BuiltIn(root, dir, tools.Options{MaxResultBytes: 4000})
	a := New(llm.NewClient(server.URL+"/v1", ""), registry, "test-model", opts, sink)
	a.opts.Audit = logger
	a.opts.Root = dir
	return a, func() { server.Close(); root.Close() }, dir, logger
}

func TestACallIsLoggedBeforeAndAfterItRuns(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"note.txt"}`}, "tool_calls"),
		sse("", "read it", "", nil, "stop"),
	}}
	a, done, dir, logger := newAuditedAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 128, CtxSize: 8000}, &captureSink{})
	defer done()
	writeLines(t, dir, "note.txt", 3)

	if err := a.Ask(context.Background(), "read note.txt"); err != nil {
		t.Fatal(err)
	}
	entries, err := logger.Read("stest")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want a started line and a result line, got %d: %+v", len(entries), entries)
	}
	started, final := entries[0], entries[1]
	if started.Status != "started" || final.Status != "ok" {
		t.Errorf("statuses are %q then %q", started.Status, final.Status)
	}
	if started.Tool != "read_file" || started.ID != "r1" || final.ID != "r1" {
		t.Errorf("the call is not identified: %+v", started)
	}
	if len(started.Paths) != 1 || filepath.Base(started.Paths[0]) != "note.txt" {
		t.Errorf("the path was not recorded: %v", started.Paths)
	}
	if started.Decision != "auto" {
		t.Errorf("a read needed no approval, decision says %q", started.Decision)
	}
	if final.ResultBytes == 0 || final.ResultSHA == "" {
		t.Errorf("the result was not sized: %+v", final)
	}
}

func TestARefusalIsInLogEvenThoughNothingRan(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "write_file", []string{`{"path":"out.txt","content":"x"}`}, "tool_calls"),
		sse("", "understood", "", nil, "stop"),
	}}
	sink := &captureSink{decision: Refuse}
	a, done, dir, logger := newAuditedAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 128, CtxSize: 8000}, sink)
	defer done()

	if err := a.Ask(context.Background(), "write out.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "out.txt")); err == nil {
		t.Fatal("a refused write still touched the disk")
	}
	entries, err := logger.Read("stest")
	if err != nil {
		t.Fatal(err)
	}
	var refused *audit.Entry
	for i, e := range entries {
		if e.Status == "refused" {
			refused = &entries[i]
		}
	}
	if refused == nil {
		t.Fatalf("no refusal in the log: %+v", entries)
	}
	if refused.Decision != "user_no" || refused.Tool != "write_file" {
		t.Errorf("the refusal is recorded wrong: %+v", refused)
	}
}

func TestWhatACommandDidAndWhatAChangeDidAreBothRecorded(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "exec_command", []string{`{"command":"exit 3"}`}, "tool_calls"),
		sse("", "", "write_file", []string{`{"path":"a.txt","content":"one\n"}`}, "tool_calls"),
		sse("", "", "write_file", []string{`{"path":"a.txt","content":"two\n"}`}, "tool_calls"),
		sse("", "done", "", nil, "stop"),
	}}
	a, done, _, logger := newAuditedAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 128, CtxSize: 8000, AssumeYes: true}, &captureSink{})
	defer done()

	if err := a.Ask(context.Background(), "run it then write"); err != nil {
		t.Fatal(err)
	}
	entries, err := logger.Read("stest")
	if err != nil {
		t.Fatal(err)
	}
	var exit *audit.Entry
	var change *audit.Entry
	for i, e := range entries {
		switch {
		case e.Tool == "exec_command" && e.Status != "started":
			exit = &entries[i]
		case e.Tool == "write_file" && e.Status != "started":
			change = &entries[i] // the last one overwrites, so it carries a diff
		}
	}
	if exit == nil || exit.ExitCode == nil || *exit.ExitCode != 3 {
		t.Errorf("the exit code is missing: %+v", exit)
	}
	if exit.Decision != "yes_flag" {
		t.Errorf("--yes should be recorded as the decision, got %q", exit.Decision)
	}
	if change == nil || change.Diff == "" {
		t.Errorf("the diff was not recorded: %+v", change)
	}
}

func TestAResumedConversationIsStillTalkingToTheModel(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")

	// first run: read a file, answer, and leave a log behind
	firstStub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"note.txt"}`}, "tool_calls"),
		sse("", "it says 90", "", nil, "stop"),
	}}
	log := session.New(sessions, "s20260101-120000")
	a, done, work, _ := newAuditedAgent(t, firstStub, Options{MaxTurns: 3, MaxTokens: 128, CtxSize: 8000}, &captureSink{})
	defer done()
	writeLines(t, work, "note.txt", 3)
	a.opts.Session = log
	if err := a.Ask(context.Background(), "what does note.txt say"); err != nil {
		t.Fatal(err)
	}

	// second run: same conversation loaded back, one question later
	saved, err := session.Load(log.Path())
	if err != nil {
		t.Fatal(err)
	}
	secondStub := &stubServer{scripts: []string{sse("", "still 90", "", nil, "stop")}}
	b, doneB, _, _ := newAuditedAgent(t, secondStub, Options{MaxTurns: 2, MaxTokens: 128, CtxSize: 8000}, &captureSink{})
	defer doneB()
	b.opts.Session = session.New(sessions, "s20260101-120000")
	b.Resume(saved)
	if err := b.Ask(context.Background(), "and again?"); err != nil {
		t.Fatal(err)
	}

	messages := secondStub.lastRequest(t)["messages"].([]any)
	var contents []string
	for _, raw := range messages {
		contents = append(contents, raw.(map[string]any)["content"].(string))
	}
	if !strings.Contains(strings.Join(contents, "\n"), "what does note.txt say") {
		t.Errorf("the earlier question is missing from the request:\n%s", strings.Join(contents, "\n"))
	}
	if strings.Contains(strings.Join(contents, "\n"), "and again?") == false {
		t.Error("the new question was not asked")
	}
}

func TestAResumeFixesWhatABadStopLeftBehind(t *testing.T) {
	saved := []llm.Message{
		{Role: "user", Content: "start"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":`)}}}},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c2", Type: "function", Function: llm.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}}}},
		{Role: "tool", Content: "package main", ToolCallID: "c2", Name: "read_file"},
	}
	stub := &stubServer{scripts: []string{sse("", "ok", "", nil, "stop")}}
	a, done, _, _ := newAuditedAgent(t, stub, Options{MaxTurns: 2, MaxTokens: 64, CtxSize: 8000}, &captureSink{})
	defer done()
	a.Resume(saved)
	if err := a.Ask(context.Background(), "and again?"); err != nil {
		t.Fatal(err)
	}

	messages := stub.lastRequest(t)["messages"].([]any)
	var toolAnswers int
	for _, raw := range messages {
		msg := raw.(map[string]any)
		if msg["role"] == "tool" {
			toolAnswers++
		}
	}
	if toolAnswers != 2 {
		t.Errorf("every call still owes a reply: %d tool messages for %d calls", toolAnswers, 2)
	}
}

func TestAResumeThatOverflowsTheWindowIsTrimmed(t *testing.T) {
	var saved []llm.Message
	for i := 0; i < 12; i++ {
		saved = append(saved, llm.Message{Role: "user", Content: strings.Repeat("a", 900)})
		saved = append(saved, llm.Message{Role: "assistant", Content: strings.Repeat("b", 900)})
	}
	stub := &stubServer{scripts: []string{sse("", "ok", "", nil, "stop")}}
	a, done, _, _ := newAuditedAgent(t, stub, Options{MaxTurns: 2, MaxTokens: 64, CtxSize: 4000}, &captureSink{})
	defer done()
	a.Resume(saved)

	if len(a.history) >= len(saved) {
		t.Fatalf("a conversation four times the window should shed turns, kept %d of %d", len(a.history), len(saved))
	}
	if a.history[len(a.history)-1].Content != saved[len(saved)-1].Content {
		t.Error("the newest turn must survive a resume that overflows")
	}
}

func TestTrimmingNeverEatsTheQuestionBeingAnswered(t *testing.T) {
	// a conversation too big for the window sheds its oldest turns, but the question being asked
	// stays: an honest "this does not fit" beats quietly answering something else
	var saved []llm.Message
	for i := 0; i < 6; i++ {
		saved = append(saved, llm.Message{Role: "user", Content: strings.Repeat("o", 1200)})
		saved = append(saved, llm.Message{Role: "assistant", Content: strings.Repeat("p", 1200)})
	}
	stub := &stubServer{scripts: []string{sse("", "ok", "", nil, "stop")}}
	a, done, _, _ := newAuditedAgent(t, stub, Options{MaxTurns: 2, MaxTokens: 64, CtxSize: 2600}, &captureSink{})
	defer done()
	a.Resume(saved)
	if err := a.Ask(context.Background(), strings.Repeat("question ", 280)); err == nil {
		t.Fatal("a conversation larger than the window should say so")
	}
	var asked bool
	for _, m := range a.history {
		if m.Role == "user" && strings.Contains(m.Content, "question question") {
			asked = true
		}
	}
	if !asked {
		t.Error("trimming removed the turn the model is supposed to answer")
	}
	if len(a.history) >= len(saved)+2 {
		t.Errorf("nothing was shed: %d messages", len(a.history))
	}
}

func TestAnsweringOnceStopsTheAskingForThatTool(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "write_file", []string{`{"path":"a.txt","content":"1"}`}, "tool_calls"),
		sse("", "", "write_file", []string{`{"path":"b.txt","content":"2"}`}, "tool_calls"),
		sse("", "both written", "", nil, "stop"),
	}}
	sink := &captureSink{decision: AllowAlways}
	a, done, dir, _ := newAuditedAgent(t, stub, Options{MaxTurns: 4, MaxTokens: 128, CtxSize: 8000}, sink)
	defer done()

	if err := a.Ask(context.Background(), "write two files"); err != nil {
		t.Fatal(err)
	}
	var confirms int
	for _, ev := range sink.events {
		if ev.Kind == Confirm {
			confirms++
		}
	}
	if confirms != 1 {
		t.Errorf("answered once, asked %d times", confirms)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was not written: %v", name, err)
		}
	}
}

func TestACommandPrefixIsAllowedWithoutItsNeighbours(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "exec_command", []string{`{"command":"go test ./..."}`}, "tool_calls"),
		sse("", "", "exec_command", []string{`{"command":"rm -rf build"}`}, "tool_calls"),
		sse("", "", "exec_command", []string{`{"command":"go mod tidy"}`}, "tool_calls"),
		sse("", "done", "", nil, "stop"),
	}}
	sink := &captureSink{decision: Refuse}
	a, done, _, _ := newAuditedAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 128, CtxSize: 8000, Allow: []string{"exec:go test"}}, sink)
	defer done()

	if err := a.Ask(context.Background(), "run these"); err != nil {
		t.Fatal(err)
	}
	var asked []string
	for _, ev := range sink.events {
		if ev.Kind == Confirm {
			asked = append(asked, ev.Args)
		}
	}
	if len(asked) != 2 {
		t.Fatalf("go test was pre-approved but its neighbours were not: asked %v", asked)
	}
	if strings.Contains(asked[0], "go test") {
		t.Errorf("`go test ./...` was asked about anyway: %v", asked)
	}
	if !strings.Contains(asked[1], "go mod tidy") {
		t.Errorf("`go mod tidy` should not be covered by a `go test` allowance: %v", asked)
	}
}

func TestAnAlwaysNeverCoversAPathOutsideTheProject(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("top secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	stub := &stubServer{scripts: []string{
		sse("", "", "write_file", []string{`{"path":"a.txt","content":"1"}`}, "tool_calls"),
		sse("", "", "write_file", []string{`{"path":"b.txt","content":"2"}`}, "tool_calls"),
		sse("", "", "read_file", []string{`{"path":"` + filepath.Join(outside, "secret.txt") + `"}`}, "tool_calls"),
		sse("", "done", "", nil, "stop"),
	}}
	sink := &captureSink{decision: AllowAlways}
	a, done, _, _ := newAuditedAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 128, CtxSize: 8000}, sink)
	defer done()

	if err := a.Ask(context.Background(), "write two files then read outside"); err != nil {
		t.Fatal(err)
	}
	var confirms []Event
	for _, ev := range sink.events {
		if ev.Kind == Confirm {
			confirms = append(confirms, ev)
		}
	}
	// one ask for the first write, nothing for the second, and the outside path asked again
	if len(confirms) != 2 {
		t.Fatalf("asked %d times, want 2: %+v", len(confirms), confirms)
	}
	if len(confirms[0].Target) != 0 {
		t.Errorf("the in-project write should not be an outside-root question: %+v", confirms[0])
	}
	if len(confirms[1].Target) == 0 {
		t.Errorf("the outside read should still have been asked about: %+v", confirms[1])
	}
}
