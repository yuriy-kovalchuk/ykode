package agent

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

func TestTruncatedToolCallIsNotRun(t *testing.T) {
	// The arguments stop mid-string: running this would send broken JSON.
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":`}, "length"),
		sse("", "recovered", "", nil, "stop"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 512}, &captureSink{})
	defer done()

	if err := a.Ask(context.Background(), "read it"); err != nil {
		t.Fatal(err)
	}
	if len(stub.requests) != 3 {
		// turn 1 truncated, turn 2 answered without a tool, turn 3 is the verification retry
		t.Fatalf("requests = %d, want the truncated turn, the answer, and the verification retry", len(stub.requests))
	}
	next := stub.requests[1]["messages"].([]any)
	last := next[len(next)-1].(map[string]any)
	if last["role"] != "user" || !strings.Contains(fmt.Sprint(last["content"]), "cut off") {
		t.Fatalf("the model was never told its reply was cut off, or was told in the wrong voice: %v", last)
	}
}

func TestOutsideRootIsConfirmedEvenWithAssumeYes(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "outside.txt"), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"../outside.txt"}`}, "tool_calls"),
		sse("", "fine", "", nil, "stop"),
	}}
	opts := Options{MaxTurns: 3, MaxTokens: 512, AssumeYes: true}
	server := httptest.NewServer(stub)
	defer server.Close()
	root, err := os.OpenRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	sink := &captureSink{decision: Refuse}
	a := New(llm.NewClient(server.URL+"/v1", ""),
		tools.NewRegistry(tools.NewReadFile(root, project)), "test-model", opts, sink)

	if err := a.Ask(context.Background(), "read the file above the root"); err != nil {
		t.Fatal(err)
	}
	if len(sink.asked) != 1 || !strings.Contains(sink.asked[0].Text, filepath.Join(dir, "outside.txt")) {
		t.Fatalf("confirmation asked = %v, want one question naming the resolved outside path", sink.asked)
	}
	if got := sink.asked[0].Target; len(got) != 1 || got[0] != filepath.Join(dir, "outside.txt") {
		t.Fatalf("confirm targets = %v, want the outside path listed for the consumer", got)
	}
	// The refusal must reach the model as the tool result.
	var refusal string
	for _, raw := range stub.lastRequest(t)["messages"].([]any) {
		m := raw.(map[string]any)
		if m["role"] == "tool" {
			refusal = fmt.Sprint(m["content"])
		}
	}
	if !strings.Contains(refusal, "refused") {
		t.Fatalf("tool result = %q, want the refusal", refusal)
	}
}

func TestARequestAfterACutOffTurnDoesNotEndWithAnAssistantLine(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":`}, "length"),
		sse("", "recovered", "", nil, "stop"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 512, NoVerify: true}, &captureSink{})
	defer done()

	if err := a.Ask(context.Background(), "read it"); err != nil {
		t.Fatal(err)
	}
	if len(stub.requests) != 2 {
		t.Fatalf("requests = %d, want the cut-off turn and the retry", len(stub.requests))
	}
	messages := stub.requests[1]["messages"].([]any)
	last := messages[len(messages)-1].(map[string]any)
	if last["role"] != "user" {
		t.Errorf("the retry request ends with role %v: a server sees that as its own unfinished reply and continues it\n%v",
			last["role"], last["content"])
	}
	if !strings.Contains(fmt.Sprint(last["content"]), "cut off") {
		t.Errorf("the retry request does not end with the notice:\n%v", last["content"])
	}
}

// exec_command starts in the project root, so a cd into it is dropped; a cd anywhere else is kept
func TestACDIntoTheProjectRootIsDropped(t *testing.T) {
	const root = "/work/proj"
	for in, want := range map[string]string{
		"cd /work/proj && go test ./...":  "go test ./...",
		"cd /work/proj; ls -la":           "ls -la",
		`cd "/work/proj" && make build`:   "make build",
		"cd '/work/proj/' && make":        "make",
		"cd . && ls":                      "ls",
		"cd /work/proj && cd sub && ls":   "cd sub && ls", // the second cd is the command's own
		"cd /work/other && ls":            "cd /work/other && ls",
		"cd sub && ls":                    "cd sub && ls",
		"cd /work/proj":                   "cd /work/proj", // nothing follows: left as it is
		"ls /work/proj":                   "ls /work/proj",
		"echo cd /work/proj && echo done": "echo cd /work/proj && echo done",
	} {
		if got := withoutRootCD(in, root); got != want {
			t.Errorf("%q became %q, want %q", in, got, want)
		}
	}
}

// the rewrite happens to the arguments the rest of the run sees, so the row, the audit log and the
// allow list all read the command without the cd
func TestAnExecCallLosesItsCDBeforeAnyoneReadsIt(t *testing.T) {
	a := &Agent{opts: Options{Root: "/work/proj"}}
	args := a.normalizedArgs("exec_command", []byte(`{"command":"cd /work/proj && go test ./...","timeout":30}`))
	if got := commandOf(string(args)); got != "go test ./..." {
		t.Errorf("command = %q", got)
	}
	if !strings.Contains(string(args), `"timeout":30`) {
		t.Errorf("another argument was lost: %s", args)
	}
	if key := callKey("exec_command", string(args)); key != "exec:go test ./..." {
		t.Errorf("the allow key is %q: with the cd gone a line can be allowed by what it runs", key)
	}
	untouched := []byte(`{"path":"cd /work/proj && x"}`)
	if got := a.normalizedArgs("read_file", untouched); string(got) != string(untouched) {
		t.Errorf("another tool's arguments were rewritten: %s", got)
	}
}
