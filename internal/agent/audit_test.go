package agent

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/audit"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// newAuditedAgent wires an agent whose audit log sits in a temp dir; the repair and
// permission tests build on it too.
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

func TestHistoryCanWidenToEveryRunOfTheRootAndAnswerOneID(t *testing.T) {
	a, done, dir, logger := newAuditedAgent(t, &stubServer{}, Options{MaxTurns: 1, CtxSize: 8000}, &captureSink{})
	defer done()
	if err := logger.Append(audit.Entry{ID: "r1", Tool: "read_file", Args: `{"path":"mine.txt"}`, Status: "ok", Root: dir}); err != nil {
		t.Fatal(err)
	}
	other := audit.New(filepath.Join(dir, "audit"), "sother")
	if err := other.Append(audit.Entry{ID: "r9", Tool: "exec_command", Args: `{"command":"go test"}`, Status: "ok", Root: dir}); err != nil {
		t.Fatal(err)
	}

	if got := a.History("", historyLimit); !strings.Contains(got, "r1") || strings.Contains(got, "r9") {
		t.Errorf("this run only: %s", got)
	}
	if got := a.History("--all", historyLimit); !strings.Contains(got, "r9") {
		t.Errorf("--all should reach the other run: %s", got)
	}
	if got := a.History("r1", historyLimit); !strings.Contains(got, "mine.txt") {
		t.Errorf("an id of this run should answer in full: %s", got)
	}
	if got := a.History("--all r9", historyLimit); !strings.Contains(got, "go test") {
		t.Errorf("--all plus an id should answer in full: %s", got)
	}
	if got := a.History("exec", historyLimit); strings.Contains(got, "r1") {
		t.Errorf("a tool filter should narrow: %s", got)
	}
}
