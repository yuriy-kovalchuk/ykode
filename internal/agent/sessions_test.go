package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
)

// /sessions names the conversations that can be resumed, marking this project's and this run's,
// because an id is a timestamp nobody picks out of a list
func TestSessionsNamesWhatCanBeResumed(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	here := session.New(sessions, "s20260101-120000.000", dir)
	elsewhere := session.New(sessions, "s20260101-110000.000", "/somewhere/else")
	for _, saved := range []struct {
		log *session.Log
		ask string
	}{{here, "this project's ask"}, {elsewhere, "another project's ask"}} {
		if err := saved.log.Append(llm.Message{Role: "user", Content: saved.ask}); err != nil {
			t.Fatal(err)
		}
		if err := saved.log.Append(llm.Message{Role: "assistant", Content: "done"}); err != nil {
			t.Fatal(err)
		}
	}
	a, done := newAgentIn(t, &stubServer{}, Options{Session: here, Root: dir}, &captureSink{}, dir)
	defer done()
	a.add(llm.Message{Role: "user", Content: "what this run is doing"})

	text, _, err := a.Command(context.Background(), "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	own := sessionsRow(text, "s20260101-120000.000")
	if !strings.Contains(own, "*") || !strings.Contains(own, "(this run)") {
		t.Errorf("this run's conversation is not marked out: %q", own)
	}
	if other := sessionsRow(text, "s20260101-110000.000"); strings.Contains(other, "*") {
		t.Errorf("another project's conversation is marked as ours: %q", other)
	}
	for _, want := range []string{"--continue", "this project's ask", "another project's ask"} {
		if !strings.Contains(text, want) {
			t.Errorf("the listing is missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "120000") > strings.Index(text, "110000") {
		t.Error("the newest conversation is not first")
	}
}

// a run that saves nothing says so instead of an empty list
func TestSessionsWithoutASavedConversation(t *testing.T) {
	dir := t.TempDir()
	a, done := newAgentIn(t, &stubServer{}, Options{}, &captureSink{}, dir)
	defer done()

	text, _, err := a.Command(context.Background(), "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "no conversation") {
		t.Errorf("sessions off answered: %q", text)
	}
}

func sessionsRow(listing, id string) string {
	for _, line := range strings.Split(listing, "\n") {
		if strings.Contains(line, id) {
			return line
		}
	}
	return ""
}

// coming back to a saved conversation takes the run over: the model reads that one, what follows is
// saved to its file, and the conversation this run gave up is left whole to come back to again
func TestContinueFromTakesTheRunOver(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	own := saveConversation(t, sessions, "s20260101-120000.000", dir, "what this run is doing", "and its answer")
	back := saveConversation(t, sessions, "s20260101-110000.000", dir, "the ask that was saved", "what it got")
	a, done := newAgentIn(t, &stubServer{}, Options{Session: own, Root: dir}, &captureSink{}, dir)
	defer done()

	if err := a.ContinueFrom(sessions, "s20260101-110000.000"); err != nil {
		t.Fatal(err)
	}
	if got := a.Conversation(); len(got) != 3 || got[1].Content != "the ask that was saved" {
		t.Errorf("the run reads %d messages, the second being %q", len(got), got[min(1, len(got)-1)].Content)
	}
	if a.SessionFile() != back.Path() {
		t.Errorf("the run now writes to %q, not %q", a.SessionFile(), back.Path())
	}

	a.add(llm.Message{Role: "assistant", Content: "carried on"})
	if again := loaded(t, back.Path(), dir); len(again) != 3 || again[2].Content != "carried on" {
		t.Errorf("the continued conversation reads back %d messages, ending %q", len(again), lastContent(again))
	}
	if left := loaded(t, own.Path(), dir); len(left) != 2 || left[0].Content != "what this run is doing" {
		t.Errorf("the conversation this run gave up lost something: %d messages, ending %q", len(left), lastContent(left))
	}
}

// another project's conversation is not this run's to continue, and the refusal says where it belongs
func TestContinueFromRefusesAnotherProjectsConversation(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	saveConversation(t, sessions, "s20260101-110000.000", "/somewhere/else", "theirs", "their answer")
	own := session.New(sessions, "s20260101-120000.000", dir)
	a, done := newAgentIn(t, &stubServer{}, Options{Session: own, Root: dir}, &captureSink{}, dir)
	defer done()
	a.add(llm.Message{Role: "user", Content: "ours"})

	err := a.ContinueFrom(sessions, "s20260101-110000.000")
	if err == nil || !strings.Contains(err.Error(), "/somewhere/else") {
		t.Errorf("another project's conversation was accepted: %v", err)
	}
	if got := a.Conversation(); len(got) != 2 || got[1].Content != "ours" {
		t.Errorf("the refused run lost its own conversation: %d messages", len(got))
	}
	if a.SessionFile() != own.Path() {
		t.Errorf("the refused run is now saving to %q", a.SessionFile())
	}
}

// a file that holds nothing cannot be continued: the run keeps what it has rather than an empty screen
func TestContinueFromRefusesAnEmptyConversation(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	own := session.New(sessions, "s20260101-120000.000", dir)
	empty := session.New(sessions, "s20260101-110000.000", dir)
	if err := empty.Cleared(); err != nil { // a conversation someone cleared: its file stands, holding nothing
		t.Fatal(err)
	}
	a, done := newAgentIn(t, &stubServer{}, Options{Session: own, Root: dir}, &captureSink{}, dir)
	defer done()
	a.add(llm.Message{Role: "user", Content: "ours"})

	if err := a.ContinueFrom(sessions, "s20260101-110000.000"); err == nil {
		t.Error("an empty conversation was continued")
	}
	if got := a.Conversation(); len(got) != 2 || got[1].Content != "ours" {
		t.Errorf("the refused run lost its own conversation: %d messages", len(got))
	}
}

// saveConversation writes one, with the ask first.
func saveConversation(t *testing.T, dir, id, root, ask, answer string) *session.Log {
	t.Helper()
	log := session.New(dir, id, root)
	for _, said := range []llm.Message{{Role: "user", Content: ask}, {Role: "assistant", Content: answer}} {
		if err := log.Append(said); err != nil {
			t.Fatal(err)
		}
	}
	return log
}

func loaded(t *testing.T, path, root string) []llm.Message {
	t.Helper()
	messages, err := session.Load(path, root)
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

func lastContent(messages []llm.Message) string {
	if len(messages) == 0 {
		return ""
	}
	return messages[len(messages)-1].Content
}

// A trim takes the oldest turns out of the conversation and writes the whole thing back each time, so a
// session that trims a dozen times carries a dozen copies of itself. The partial record the format
// already replayed is the fix: one line naming positions, and a resume that reads back exactly what the
// run is holding.
func TestATrimSavesOneLineAndStillReadsBackWhole(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	log := session.New(sessions, "s20260101-140000.000", dir)
	a, done := newAgentIn(t, &stubServer{}, Options{Session: log, Root: dir,
		CtxSize: (len(SystemPrompt) + 900) / 4}, &captureSink{}, dir)
	defer done()

	a.add(llm.Message{Role: "user", Content: "read them all"})
	for _, id := range []string{"c1", "c2", "c3"} {
		a.add(llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id,
			Function: llm.FunctionCall{Name: "read_file", Arguments: []byte(`{"path":"f.go"}`)}}}})
		a.add(llm.Message{Role: "tool", ToolCallID: id, Name: "read_file",
			Content: strings.Repeat("x", 300)})
	}
	a.makeRoom()

	if blocks := callBlocks(a.history); blocks >= 3 {
		t.Fatalf("%d tool calls left, want the trim to have run", blocks)
	}
	sizeBefore := fileSize(t, log.Path())
	a.add(llm.Message{Role: "user", Content: "and now again"})
	a.makeRoom()
	if grew := fileSize(t, log.Path()) - sizeBefore; grew > 4096 {
		t.Errorf("one more trim grew the log by %d bytes, want a short line rather than a copy", grew)
	}

	back := loaded(t, log.Path(), dir)
	if len(back) != len(a.history)-1 {
		t.Fatalf("the log holds %d messages, the conversation holds %d", len(back), len(a.history)-1)
	}
	for i := range back {
		if back[i].Content != a.history[i+1].Content || back[i].ToolCallID != a.history[i+1].ToolCallID {
			t.Errorf("message %d differs: log %q/%q, live %q/%q", i, back[i].Role, back[i].Content,
				a.history[i+1].Role, a.history[i+1].Content)
		}
	}
}

// A repaired call is a replacement at a position, not a new message: what the file reads back carries
// the repair and nothing else has moved.
func TestARepairedCallIsReadBackRepaired(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	log := session.New(sessions, "s20260101-140100.000", dir)
	a, done := newAgentIn(t, &stubServer{}, Options{Session: log, Root: dir}, &captureSink{}, dir)
	defer done()

	a.add(llm.Message{Role: "user", Content: "read it"})
	a.add(llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1",
		Function: llm.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"f`)}}}})
	a.add(llm.Message{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: "the result"})
	a.sanitizeHistory()

	back := loaded(t, log.Path(), dir)
	if len(back) != 3 {
		t.Fatalf("the log holds %d messages, want the three that were said", len(back))
	}
	if args := string(back[1].ToolCalls[0].Function.Arguments); args != "{}" {
		t.Errorf("the saved call holds %q, want the repaired arguments", args)
	}
	if back[2].Content != "the result" {
		t.Errorf("a message after the repaired one moved: %q", back[2].Content)
	}
}

func fileSize(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return len(raw)
}
