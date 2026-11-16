package session

import (
	"encoding/json"
	"fmt"
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
		{Role: "assistant", Content: "fixed", Reasoning: "this thinking is saved"},
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
	if back[3].Reasoning != "this thinking is saved" {
		t.Errorf("thinking did not come back: %q", back[3].Reasoning)
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
	if err := log.Folded(Fold{Kept: kept}); err != nil {
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

// thinking is saved beside the message it came with, and a fold keeps it and says what it did
func TestThinkingAndAFoldSurviveAResume(t *testing.T) {
	dir := t.TempDir()
	log := New(dir, "s1", "/p")
	if err := log.Append(llm.Message{Role: "assistant", Content: "done", Reasoning: "first I check a.go"}); err != nil {
		t.Fatal(err)
	}
	kept := []llm.Message{{Role: "user", Content: "recap"}, {Role: "assistant", Content: "next", Reasoning: "then b.go"}}
	if err := log.Folded(Fold{Kept: kept, Summary: "recap", ReadFiles: []string{"a.go"}, ModifiedFiles: []string{"b.go"}, Dropped: 7, TokensBefore: 9000}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "s1.jsonl"))
	for _, want := range []string{`"thinking":"first I check a.go"`, `"dropped":7`, `"modified_files":["b.go"]`, `"tokens_before":9000`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("the log lacks %s:\n%s", want, raw)
		}
	}
	got, err := Load(filepath.Join(dir, "s1.jsonl"), "/p")
	if err != nil || len(got) != 2 || got[1].Reasoning != "then b.go" {
		t.Fatalf("after the fold: %+v, %v", got, err)
	}
	if !strings.Contains(string(mustJSON(got[1])), `"reasoning_content":"then b.go"`) {
		t.Error("the thinking does not go back to the model after a resume")
	}
}

// the note ykode ends an ask with is split off again wherever an ask is shown to a person
func TestTheNoteAtTheEndOfAnAskIsSplitOffAgain(t *testing.T) {
	note := "date: 2026-11-09\ngit: branch main, clean"
	ask := "fix the bug\n\nin two lines" + EnvironmentOpen + note + EnvironmentClose
	text, got := SplitEnvironment(ask)
	if text != "fix the bug\n\nin two lines" || got != note {
		t.Errorf("split into %q and %q", text, got)
	}
	if WithoutEnvironment(ask) != text {
		t.Error("WithoutEnvironment disagrees with the split")
	}
	for _, plain := range []string{"just an ask", "mentions <environment> in passing", "ends oddly\n\n<environment>\nno close"} {
		if text, note := SplitEnvironment(plain); text != plain || note != "" {
			t.Errorf("%q was split into %q and %q", plain, text, note)
		}
	}
}

// what a run did to its conversation after saying it is read back with it: a message put in another's
// place, messages taken out, and the whole of it written again
func TestChangesToTheConversationAreReadBack(t *testing.T) {
	dir := t.TempDir()
	log := New(dir, "s1", "/work")
	said := func(i int) llm.Message { return llm.Message{Role: "user", Content: fmt.Sprintf("message %d", i)} }
	for i := 0; i < 5; i++ {
		if err := log.Append(said(i)); err != nil {
			t.Fatal(err)
		}
	}
	read := func() string {
		t.Helper()
		messages, err := Load(filepath.Join(dir, "s1.jsonl"), "/work")
		if err != nil {
			t.Fatal(err)
		}
		var texts []string
		for _, m := range messages {
			texts = append(texts, m.Content)
		}
		return strings.Join(texts, " | ")
	}
	// a run that rewrote the conversation in place wrote these lines; nothing writes them now,
	// and an older file still reads back the same way
	old := func(v any) {
		t.Helper()
		if err := log.write(mustJSON(v)); err != nil {
			t.Fatal(err)
		}
	}
	at := 1
	shortened := recordOf(llm.Message{Role: "user", Content: "shortened"})
	old(change{Revised: &at, Message: &shortened})
	old(change{Removed: []int{2, 4}})
	if got, want := read(), "message 0 | shortened | message 4"; got != want {
		t.Fatalf("read back %q, want %q", got, want)
	}
	// a change that points outside the conversation is left out, by the reader and by the listing alike
	outside := 7
	old(change{Removed: []int{2, 9}})
	seventh := recordOf(said(7))
	old(change{Revised: &outside, Message: &seventh})
	if got, want := read(), "message 0 | shortened | message 4"; got != want {
		t.Fatalf("a change that fits nothing moved the conversation: %q", got)
	}
	list, err := Describe(dir, 5)
	if err != nil || len(list) != 1 || list[0].Messages != 3 {
		t.Fatalf("the listing counts %+v, want the 3 messages a resume reads (%v)", list, err)
	}
	if err := log.Rewritten([]llm.Message{said(8), said(9)}); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(said(10)); err != nil {
		t.Fatal(err)
	}
	if got, want := read(), "message 8 | message 9 | message 10"; got != want {
		t.Fatalf("read back %q, want %q", got, want)
	}
}

// reading a conversation to resume it must not touch the file: the bytes it holds are what the
// next run's fold will be asked about, and rewriting them is how a resume loses an ask
func TestReadingASavedConversationWritesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s20260101-000000.jsonl")
	log := New(dir, "s20260101-000000", "/work")
	for i := 0; i < 4; i++ {
		if err := log.Append(llm.Message{Role: "user", Content: fmt.Sprintf("message %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path, "/work"); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("reading the conversation changed the file:\n%s\nwant\n%s", after, before)
	}
}

// thinking is written once on a line, and comes back as the thinking the model is sent
func TestThinkingIsStoredOnceAndComesBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s20260101-000000.jsonl")
	log := New(dir, "s20260101-000000", "/work")
	if err := log.Append(llm.Message{Role: "assistant", Content: "answer", Reasoning: "thought it through"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(llm.Message{Role: "assistant", Content: "next"}); err != nil {
		t.Fatal(err)
	}

	line, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(line), `"thought it through"`) != 1 {
		t.Errorf("the same thinking is written twice on one line:\n%s", line)
	}

	back, err := Load(path, "/work")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 {
		t.Fatalf("%d messages loaded, want 2", len(back))
	}
	if back[0].Reasoning != "thought it through" {
		t.Errorf("the thinking is missing after a resume: %+v", back[0])
	}
	if back[1].Reasoning != "" {
		t.Errorf("a reply with no thinking came back with some: %+v", back[1])
	}
}

// a file written before that change holds the thinking twice and still reads back whole
func TestThinkingStoredTwiceByAnOlderRunStillLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s20260101-000000.jsonl")
	line := `{"role":"assistant","content":"answer","reasoning_content":"thought it through","thinking":"thought it through"}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	back, err := Load(path, "/work")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Reasoning != "thought it through" {
		t.Fatalf("an older file did not read back: %+v", back)
	}
}

// A trim takes the oldest turns out of a conversation and writes nothing else. Recording that as one
// line by position is the point of an append-only log: a copy of the whole conversation per removal,
// each holding the full text of every file read, is what made the file grow a dozen times over.
func TestADropIsOneLineRatherThanACopyOfTheConversation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s20260101-130000.jsonl")
	log := New(dir, "s20260101-130000", "")
	for _, m := range []llm.Message{
		{Role: "user", Content: strings.Repeat("ask ", 500)},
		{Role: "assistant", Content: "read it"},
		{Role: "tool", Content: strings.Repeat("a line of the file that was read\n", 200)},
		{Role: "user", Content: "the ask that stays"},
	} {
		if err := log.Append(m); err != nil {
			t.Fatal(err)
		}
	}
	afterAppends, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Dropped(0, 3); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// the drop itself costs one short line: a whole copy of what is left would carry the 6000-odd
	// bytes of that file read again
	if grew := len(raw) - len(afterAppends); grew > 64 {
		t.Errorf("the drop grew the file by %d bytes, want one short line", grew)
	}
	back, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Content != "the ask that stays" {
		t.Errorf("resumed %d message(s) %q, want only the ask that stays", len(back), back)
	}
	if log.Count() != 1 {
		t.Errorf("Count = %d, want the log to know it holds one message now", log.Count())
	}
}

// A change points at a position counted from the last boundary, so a fold or a start-over moves where
// the next line points. Positions carried across a boundary would delete the wrong turns.
func TestAChangePointsWhereTheMessageStoodAfterTheLastBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s20260101-130100.jsonl")
	log := New(dir, "s20260101-130100", "")
	for _, ask := range []string{"an ask before", "an answer before"} {
		if err := log.Append(llm.Message{Role: "user", Content: ask}); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Folded(Fold{Kept: []llm.Message{{Role: "user", Content: "the recap"}, {Role: "user", Content: "the ask kept"}}}); err != nil {
		t.Fatal(err)
	}
	if err := log.Dropped(1, 2); err != nil { // position 1 of what the fold left, not of the file
		t.Fatal(err)
	}
	back, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Content != "the recap" {
		t.Errorf("resumed %+v, want the recap alone", back)
	}

	// and the whole conversation, not a position, is what a start-over leaves
	if err := log.Cleared(); err != nil {
		t.Fatal(err)
	}
	if err := log.Dropped(0, 1); err == nil {
		t.Error("a drop into a conversation the log says is empty was accepted")
	}
}

// A replacement message goes in by position, which is how a repaired call is kept without writing the
// conversation again.
func TestAMessageReplacedInPlaceIsReadBackReplaced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s20260101-130200.jsonl")
	log := New(dir, "s20260101-130200", "")
	for _, m := range []llm.Message{{Role: "user", Content: "first"}, {Role: "user", Content: "second"}} {
		if err := log.Append(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Revised(0, llm.Message{Role: "user", Content: "first, repaired"}); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 || back[0].Content != "first, repaired" || back[1].Content != "second" {
		t.Errorf("resumed %+v, want the first replaced and the second untouched", back)
	}
	if err := log.Revised(9, llm.Message{Role: "user", Content: "nowhere"}); err == nil {
		t.Error("a position outside the conversation was accepted")
	}
}

// A file written by a newer program than this one cannot be rebuilt correctly, and a conversation that
// looks fine and is wrong is worse than a resume that refuses.
func TestAFileFromAFutureFormatIsRefusedRatherThanGuessedAt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s20260101-130300.jsonl")
	os.WriteFile(path, []byte(`{"root":"/work/alpha","format":99}`+"\n"+`{"role":"user","content":"a message"}`+"\n"), 0o600)
	if _, err := Load(path, "/work/alpha"); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Errorf("a format this program does not know resumed anyway: %v", err)
	}

	// a file from before the field carries a zero, which this program does read
	old := filepath.Join(dir, "s20260101-130400.jsonl")
	os.WriteFile(old, []byte(`{"root":"/work/alpha"}`+"\n"+`{"role":"user","content":"an older file"}`+"\n"), 0o600)
	back, err := Load(old, "/work/alpha")
	if err != nil || len(back) != 1 {
		t.Errorf("an older file failed to resume: %v %v", back, err)
	}
}
