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
	log := New(filepath.Dir(path), "s20260101-120000", "")
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

	back, err := Load(path, "")
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
	messages, err := Load(path, "")
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
	if _, err := Load(path, ""); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("want the bad line named, got %v", err)
	}
}

func TestTheNewestConversationIsListedFirst(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	for _, id := range []string{"s20260101-120000", "s20260103-090000", "s20260102-080000"} {
		if err := New(dir, id, "").Append(llm.Message{Role: "user", Content: "x"}); err != nil {
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
	if err := New("/tmp/sessions", "", "").Append(llm.Message{Role: "user"}); err != nil {
		t.Errorf("a disabled session failed: %v", err)
	}
}

func TestASessionWithNoIDWritesNothing(t *testing.T) {
	dir := t.TempDir()
	if log := New(dir, "", ""); log != nil {
		t.Fatal("an empty id should turn sessions off")
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("nothing should exist in %s: %v", dir, entries)
	}
}

// The result ids are how recall names a saved result, so a resumed conversation has to keep them:
// without them every old result is blanked for good.
func TestASavedConversationKeepsItsResultIds(t *testing.T) {
	dir := t.TempDir()
	log := New(dir, "s20260101-000000", "")
	messages := []llm.Message{
		{Role: "user", Content: "read it"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID:       "c1",
			Type:     "function",
			Function: llm.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
		}}},
		{Role: "tool", Name: "read_file", ToolCallID: "c1", CallID: "r1", Content: "the text"},
	}
	for _, m := range messages {
		if err := log.Append(m); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Load(filepath.Join(dir, "s20260101-000000.jsonl"), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("loaded %d messages", len(got))
	}
	if got[2].CallID != "r1" {
		t.Errorf("the result id did not survive the session file: %+v", got[2])
	}
}

func TestAConversationOnlyResumesInTheProjectItBelongsTo(t *testing.T) {
	dir := t.TempDir()
	if err := New(dir, "s1", "/work/alpha").Append(llm.Message{Role: "user", Content: "fix alpha"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "s1.jsonl")

	if back, err := Load(path, "/work/alpha"); err != nil || len(back) != 1 || back[0].Content != "fix alpha" {
		t.Fatalf("its own project: %v %v", back, err)
	}
	if _, err := Load(path, "/work/beta"); err == nil || !strings.Contains(err.Error(), "/work/alpha") {
		t.Errorf("another project resumed it: %v", err)
	}
}

// a session that never answered is not worth resuming, so the log counts what it saved
func TestTheLogCountsWhatItSaved(t *testing.T) {
	dir := t.TempDir()
	log := New(dir, "s20260101-101010", dir)
	if log.Count() != 0 {
		t.Fatalf("a fresh log counts %d messages", log.Count())
	}
	for _, m := range []llm.Message{{Role: "user", Content: "one"}, {Role: "assistant", Content: "two"}} {
		if err := log.Append(m); err != nil {
			t.Fatal(err)
		}
	}
	if log.Count() != 2 {
		t.Errorf("Count = %d, want the two messages saved", log.Count())
	}
	var off *Log
	if off.Count() != 0 {
		t.Error("a log that is off counts something")
	}
}

// starting over has to reach the saved conversation too: a resume must not hand back what was cleared
func TestStartingOverLeavesNothingToResume(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions", "s20260101-120000.jsonl")
	log := New(filepath.Dir(path), "s20260101-120000", "")
	for _, ask := range []string{"an ask that was cleared", "an answer that was cleared"} {
		if err := log.Append(llm.Message{Role: "user", Content: ask}); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Cleared(); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(llm.Message{Role: "user", Content: "a fresh ask"}); err != nil {
		t.Fatal(err)
	}

	back, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Content != "a fresh ask" {
		t.Errorf("the resume hands back what was cleared: %+v", back)
	}
}

// a fold is what the run ended with, so a resume starts there rather than reloading and re-trimming
// what the last run already put away
func TestAFoldIsSavedSoAResumeStartsFromIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions", "s20260101-120000.jsonl")
	log := New(filepath.Dir(path), "s20260101-120000", "")
	for _, old := range []string{"an older ask", "its long answer", "the tool result it needed"} {
		if err := log.Append(llm.Message{Role: "user", Content: old}); err != nil {
			t.Fatal(err)
		}
	}
	kept := []llm.Message{
		{Role: "user", Content: "[ykode compacted the earlier turns into this summary]\nGoal: the fix"},
		{Role: "user", Content: "the ask still in hand"},
	}
	if err := log.Folded(kept); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(llm.Message{Role: "assistant", Content: "kept answer"}); err != nil {
		t.Fatal(err)
	}

	back, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 3 {
		t.Fatalf("a resume reads back %d messages, want the two kept plus the one that followed: %+v", len(back), back)
	}
	if !strings.Contains(back[0].Content, "Goal: the fix") {
		t.Errorf("the fold's recap was lost: %q", back[0].Content)
	}
	if back[2].Content != "kept answer" {
		t.Errorf("what followed the fold is out of order: %+v", back)
	}
	for _, m := range back {
		if strings.Contains(m.Content, "the tool result it needed") {
			t.Error("what was folded away is back")
		}
	}
}
