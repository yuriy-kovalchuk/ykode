package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

func TestThinkingAndAnswerGoToDifferentStreams(t *testing.T) {
	var out, log bytes.Buffer
	v := NewView(&out, &log, nil)

	v.Emit(agent.Event{Kind: agent.Thinking, Text: "pondering "})
	v.Emit(agent.Event{Kind: agent.Thinking, Text: "the problem"})
	v.Emit(agent.Event{Kind: agent.Answer, Text: "the answer"})
	v.Emit(agent.Event{Kind: agent.TurnEnd, Stats: agent.Stats{
		PromptTokens:  312,
		CachedTokens:  12,
		OutputTokens:  90,
		PrefillTokSec: 454,
		DecodeTokSec:  35,
		FinishReason:  "stop",
	}})

	if strings.Contains(out.String(), "pondering") {
		t.Fatalf("thinking leaked into the answer stream: %q", out.String())
	}
	if out.String() != "the answer\n" {
		t.Fatalf("answer stream = %q, want the answer followed by a newline", out.String())
	}
	if !strings.Contains(log.String(), "thinking: pondering the problem") {
		t.Fatalf("log = %q, want the thinking text", log.String())
	}
	for _, want := range []string{"in 312 (cached 12) / out 90", "prefill 454 tok/s", "decode 35 tok/s", "stop"} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("log = %q, want it to mention %q", log.String(), want)
		}
	}
}

func TestColoredThinkingIsClosedOnEveryTurn(t *testing.T) {
	var out, log bytes.Buffer
	v := NewView(&out, &log, nil)
	v.colored = true

	v.Emit(agent.Event{Kind: agent.Thinking, Text: "half a thought"})
	v.Emit(agent.Event{Kind: agent.TurnEnd})

	if !strings.Contains(log.String(), "\x1b[2;3m") {
		t.Fatalf("log = %q, want the dim/italic sequence for thinking", log.String())
	}
	if !strings.HasSuffix(log.String(), "\x1b[0m\n") {
		t.Fatalf("log = %q, want the colour reset at the end of the thinking block", log.String())
	}
	if out.Len() != 0 {
		t.Fatalf("answer stream = %q, want nothing", out.String())
	}
}

func TestNoFooterWhenServerGivesNoTimings(t *testing.T) {
	var out, log bytes.Buffer
	v := NewView(&out, &log, nil)
	v.Emit(agent.Event{Kind: agent.Answer, Text: "answer"})
	v.Emit(agent.Event{Kind: agent.TurnEnd})

	if log.String() != "" {
		t.Fatalf("log = %q, want no timing line", log.String())
	}
	if out.String() != "answer\n" {
		t.Fatalf("answer stream = %q", out.String())
	}
}

func TestToolEventsLandInTheLog(t *testing.T) {
	var out, log bytes.Buffer
	v := NewView(&out, &log, nil)

	v.Emit(agent.Event{Kind: agent.ToolStart, Tool: "read_file", Args: `{"path":"a.go"}`})
	v.Emit(agent.Event{Kind: agent.ToolEnd, Tool: "read_file", Text: "1 package main"})
	v.Emit(agent.Event{Kind: agent.ToolFail, Tool: "edit_file", Text: "old_text not found"})
	v.Emit(agent.Event{Kind: agent.Notice, Text: "trimmed an old tool result"})

	for _, want := range []string{"-> read_file {\"path\":\"a.go\"}", "<- read_file 1 package main", "!! edit_file: old_text not found", ".. trimmed an old tool result"} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("log = %q, want it to show %q", log.String(), want)
		}
	}
	if out.Len() != 0 {
		t.Fatalf("answer stream = %q, want tool noise kept out of it", out.String())
	}
}

func TestConfirmationIsAskedAndAnswered(t *testing.T) {
	var out, log bytes.Buffer
	lines := NewLineSource(strings.NewReader("y\n"))
	v := NewView(&out, &log, NewConfirmer(lines, &out))

	if got := v.Emit(agent.Event{Kind: agent.Confirm, Tool: "exec_command", Text: "exec_command rm -rf build"}); got != agent.Allow {
		t.Fatalf("decision = %v, want the typed y to allow it", got)
	}
	if !strings.Contains(out.String(), "exec_command rm -rf build [y/N]") {
		t.Fatalf("stdout = %q, want the question visible", out.String())
	}
}

func TestNobodyToAskMeansRefusal(t *testing.T) {
	var out, log bytes.Buffer
	v := NewView(&out, &log, nil)

	if got := v.Emit(agent.Event{Kind: agent.Confirm, Tool: "exec_command"}); got != agent.Refuse {
		t.Fatalf("decision = %v, want refusal when no one can answer", got)
	}
}
