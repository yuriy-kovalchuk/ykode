package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

func writeLog(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), filePerm); err != nil {
		t.Fatal(err)
	}
}

func TestAConversationSurvivesAReStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions", "s20260101-120000.jsonl")
	log := New(filepath.Dir(path), "s20260101-120000")
	messages := []llm.Message{
		{Role: "user", Content: "fix the bug"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}}}},
		{Role: "tool", Content: "package main", ToolCallID: "c1", Name: "read_file"},
		{Role: "assistant", Content: "fixed", Reasoning: "this thinking is not saved"},
	}
	for _, m := range messages {
		if err := log.Append(m); err != nil {
			t.Fatal(err)
		}
	}

	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != len(messages) {
		t.Fatalf("read back %d messages, want %d", len(back), len(messages))
	}
	if back[1].ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("the tool call did not survive: %+v", back[1])
	}
	if back[2].ToolCallID != "c1" {
		t.Errorf("the reply lost its call id: %+v", back[2])
	}
	if back[3].Reasoning != "" {
		t.Errorf("thinking was saved to disk: %q", back[3].Reasoning)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("session mode is %o, want 0600", info.Mode().Perm())
	}
}

func TestAHalfWrittenLastLineIsDroppedNotFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	writeLog(t, path, `{"role":"user","content":"one"}`+"\n"+`{"role":"assis`)
	messages, err := Load(path)
	if err != nil {
		t.Fatalf("a torn tail should not fail the resume: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "one" {
		t.Errorf("got %+v", messages)
	}
}

func TestABrokenLineInMiddleIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	writeLog(t, path, `{"role":"user","content":"one"}`+"\nnot json\n"+`{"role":"user","content":"two"}`+"\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("want the bad line named, got %v", err)
	}
}

func TestTheNewestConversationIsListedFirst(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	for _, id := range []string{"s20260101-120000", "s20260103-090000", "s20260102-080000"} {
		if err := New(dir, id).Append(llm.Message{Role: "user", Content: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"s20260103-090000", "s20260102-080000", "s20260101-120000"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("order is %v, want %v", ids, want)
	}
}

func TestASessionWithNoIdIsOff(t *testing.T) {
	if err := New("/tmp/sessions", "").Append(llm.Message{Role: "user"}); err != nil {
		t.Errorf("a disabled session failed: %v", err)
	}
}

func TestASessionWithNoIDWritesNothing(t *testing.T) {
	dir := t.TempDir()
	if log := New(dir, ""); log != nil {
		t.Fatal("an empty id should turn sessions off")
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("nothing should exist in %s: %v", dir, entries)
	}
}
