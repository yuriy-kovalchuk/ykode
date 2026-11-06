package agent

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	read := func(i int) string {
		return sse("", "", "read_file", []string{`{"path":"` + names[i] + `"}`}, "tool_calls")
	}
	stub := &stubServer{scripts: []string{read(0), read(1), read(2), read(3), read(4), sse("", "done", "", nil, "stop")}}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 8, MaxTokens: 128, CtxSize: 4000}, &captureSink{decision: Allow}, dir)
	defer done()

	if err := a.Ask(context.Background(), "read both files twice"); err != nil {
		t.Fatal(err)
	}
	contents := toolContents(t, stub.requests[len(stub.requests)-1])
	if len(contents) < 3 {
		t.Fatalf("%d tool results in the last request, want at least three", len(contents))
	}
	summary := contents[0]
	if !strings.HasPrefix(summary, shrinkMark) {
		t.Fatalf("the oldest result is still whole:\n%s", brief(summary))
	}
	if !strings.Contains(summary, `"id":"r1"`) || !strings.Contains(summary, "recall") {
		t.Fatalf("the summary does not say how to get the text back:\n%s", summary)
	}
	if !strings.Contains(summary, "read_file") || !strings.Contains(summary, "it began:") {
		t.Fatalf("the summary does not say what the result was:\n%s", summary)
	}
	for _, recent := range contents[len(contents)-keepResults:] {
		if strings.HasPrefix(recent, shrinkMark) {
			t.Errorf("a result the model is still working with was shrunk:\n%s", brief(recent))
		}
	}
	full, err := os.ReadFile(filepath.Join(dir, "saved", "r1.txt"))
	if err != nil {
		t.Fatalf("the full text was not kept: %v", err)
	}
	if !strings.Contains(string(full), names[0]) {
		t.Error("the kept text is not the result that went in")
	}
}

func TestCompactShortensTheRestAndSaysHowMuch(t *testing.T) {
	dir := t.TempDir()
	name := writeLines(t, dir, "first.txt", 60)
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"` + name + `"}`}, "tool_calls"),
		sse("", "done", "", nil, "stop"),
	}}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 4, MaxTokens: 128, CtxSize: 4000}, &captureSink{decision: Allow}, dir)
	defer done()

	if err := a.Ask(context.Background(), "read it"); err != nil {
		t.Fatal(err)
	}
	before := a.estimateTokens()
	if freed := a.Compact(); freed <= 0 {
		t.Fatalf("Compact freed %d tokens with a multi-kilobyte result in the history", freed)
	}
	if after := a.estimateTokens(); after >= before {
		t.Errorf("the conversation is %d tokens after Compact and %d before", after, before)
	}
	if _, err := os.ReadFile(filepath.Join(dir, "saved", "r1.txt")); err != nil {
		t.Errorf("Compact dropped the result without keeping it: %v", err)
	}
}

func brief(s string) string {
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

func TestRecallIsOnlyOfferedOnceSomethingWasShortened(t *testing.T) {
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
	if toolNames(stub.requests[0])["recall"] {
		t.Error("recall was described before anything had been shortened")
	}
	last := toolNames(stub.requests[len(stub.requests)-1])
	if !toolNames(stub.requests[0])["read_file"] {
		t.Fatal("the first request lost a tool it should always have")
	}
	if !last["recall"] {
		t.Error("recall is missing after a result was shortened")
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
