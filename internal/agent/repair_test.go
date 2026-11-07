package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
)

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

func TestAResumedRunContinuesTheResultNumbers(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{CtxSize: 8000}, &captureSink{})
	defer done()

	a.Resume([]llm.Message{
		{Role: "user", Content: "run these"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "exec_command", Arguments: json.RawMessage(`{"command":"date"}`)},
		}}},
		{Role: "tool", Name: "exec_command", ToolCallID: "c1", CallID: "r1", Content: "now"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "c2", Type: "function", Function: llm.FunctionCall{Name: "exec_command", Arguments: json.RawMessage(`{"command":"id"}`)},
		}}},
		{Role: "tool", Name: "exec_command", ToolCallID: "c2", CallID: "r7", Content: "uid 501"},
	})

	ids := a.nextCallIDs(2)
	if ids[0] != "r8" || ids[1] != "r9" {
		t.Errorf("a resumed run numbered its results %v: recall r7 could now be handed someone else's output", ids)
	}
}
