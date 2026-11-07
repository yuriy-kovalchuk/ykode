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
	roles := stub.lastRequest(t)["messages"].([]any)
	var note string
	for _, raw := range roles {
		m := raw.(map[string]any)
		if m["role"] == "assistant" && strings.Contains(fmt.Sprint(m["content"]), "cut off") {
			note = m["content"].(string)
		}
	}
	if note == "" {
		t.Fatal("the model was never told its reply was cut off")
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
