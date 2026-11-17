package agent

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
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
	if len(stub.requests) != 2 {
		t.Fatalf("requests = %d, want the truncated turn and the answer", len(stub.requests))
	}
	next := stub.requests[1]["messages"].([]any)
	last := next[len(next)-1].(map[string]any)
	if last["role"] != "tool" || !strings.Contains(fmt.Sprint(last["content"]), "cut off") {
		t.Fatalf("the cut call was not answered with why it did not run: %v", last)
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
	a, done := newAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 512}, &captureSink{})
	defer done()

	if err := a.Ask(context.Background(), "read it"); err != nil {
		t.Fatal(err)
	}
	if len(stub.requests) != 2 {
		t.Fatalf("requests = %d, want the cut-off turn and the retry", len(stub.requests))
	}
	messages := stub.requests[1]["messages"].([]any)
	last := messages[len(messages)-1].(map[string]any)
	if last["role"] == "assistant" {
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

// the rewrite happens to the arguments the rest of the run sees, so the row on the screen and the
// allow list both read the command without the cd
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

// pictureFile writes a picture the run can read: the tests need a file of the right bytes, not a name
// that only looks like one.
func pictureFile(t *testing.T, dir, name string) {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 6, 4))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// pictureURLs is the pictures a request carries, wherever the API takes them.
func pictureURLs(req map[string]any) []string {
	var urls []string
	for _, raw := range req["messages"].([]any) {
		parts, ok := raw.(map[string]any)["content"].([]any)
		if !ok {
			continue // a plain string of text carries nothing
		}
		for _, part := range parts {
			if url, ok := part.(map[string]any)["image_url"].(map[string]any); ok {
				urls = append(urls, fmt.Sprint(url["url"]))
			}
		}
	}
	return urls
}

func toolReply(t *testing.T, req map[string]any) string {
	t.Helper()
	for _, raw := range req["messages"].([]any) {
		if m, ok := raw.(map[string]any); ok && m["role"] == "tool" {
			return fmt.Sprint(m["content"])
		}
	}
	t.Fatal("the request holds no tool reply")
	return ""
}

// a picture the read opened reaches the model that reads pictures, in the shape the chat API takes:
// the tool reply keeps its text and the picture follows the run as a message of its own
func TestAPictureReadFromAFileReachesTheModelThatReadsPictures(t *testing.T) {
	dir := t.TempDir()
	pictureFile(t, dir, "shot.png")
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"shot.png"}`}, "tool_calls"),
		sse("", "it is a login form", "", nil, "stop"),
	}}
	a, done := newAgentIn(t, stub, Options{MaxTurns: 3, MaxTokens: 512, Vision: true}, &captureSink{}, dir)
	defer done()

	if err := a.Ask(context.Background(), "what is in this screenshot?"); err != nil {
		t.Fatal(err)
	}
	if len(stub.requests) != 2 {
		t.Fatalf("requests = %d, want the call and the answer", len(stub.requests))
	}
	sent := pictureURLs(stub.requests[1])
	if len(sent) != 1 {
		t.Fatalf("the second request carries %d pictures, want the one that was read", len(sent))
	}
	if !strings.HasPrefix(sent[0], "data:image/png;base64,") {
		t.Errorf("the picture went out as %q", oneLine(sent[0], 60))
	}
	if strings.Contains(toolReply(t, stub.requests[1]), "reads no pictures") {
		t.Error("the model that reads pictures was told it could not see it")
	}
}

// a model with no image reader is sent the conversation as text, told why the picture is missing, and
// the bytes stay in the conversation so a switch to a model that reads them sends them
func TestAPictureIsNotSentToAModelThatReadsNone(t *testing.T) {
	dir := t.TempDir()
	pictureFile(t, dir, "shot.png")
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"shot.png"}`}, "tool_calls"),
		sse("", "I cannot see it", "", nil, "stop"),
	}}
	a, done := newAgentIn(t, stub, Options{MaxTurns: 3, MaxTokens: 512}, &captureSink{}, dir)
	defer done()

	if err := a.Ask(context.Background(), "what is in this screenshot?"); err != nil {
		t.Fatal(err)
	}
	if sent := pictureURLs(stub.requests[1]); len(sent) != 0 {
		t.Errorf("the request carries %d pictures for a model that reads none", len(sent))
	}
	if reply := toolReply(t, stub.requests[1]); !strings.Contains(reply, "reads no pictures") {
		t.Errorf("the model was not told the picture was left out: %q", oneLine(reply, 120))
	}
	held := 0
	for _, m := range a.Conversation() {
		held += len(m.Attachments)
	}
	if held != 1 {
		t.Errorf("the conversation holds %d attachments, want the one kept for a model that can read it", held)
	}
}
