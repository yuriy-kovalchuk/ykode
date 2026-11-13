package cli

import (
	"bytes"
	"context"
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

func TestAConfirmationShowsTheQuestionAndReadsTheAnswer(t *testing.T) {
	for typed, want := range map[string]agent.Decision{"y\n": agent.Allow, "a\n": agent.AllowAlways} {
		var out, log bytes.Buffer
		v := NewView(&out, &log, NewConfirmer(NewLineSource(strings.NewReader(typed)), &out))

		if got := v.Emit(agent.Event{Kind: agent.Confirm, Tool: "exec_command", Text: "exec_command rm -rf build"}); got != want {
			t.Errorf("typed %q: decision = %v, want %v", typed, got, want)
		}
		if !strings.Contains(out.String(), "exec_command rm -rf build [y/N/a]") {
			t.Errorf("typed %q: stdout = %q, want the question with its three answers", typed, out.String())
		}
	}
}

func TestNobodyToAskMeansRefusal(t *testing.T) {
	var out, log bytes.Buffer
	v := NewView(&out, &log, nil)

	if got := v.Emit(agent.Event{Kind: agent.Confirm, Tool: "exec_command"}); got != agent.Refuse {
		t.Fatalf("decision = %v, want refusal when no one can answer", got)
	}
}

// The line front end asks in the terminal it prints to: a number picks the choice it names, typed
// words answer as they were typed, and an empty line says there is none.
func TestTheQuestionOnTheLineIsAnsweredByWhatIsTyped(t *testing.T) {
	var out, log bytes.Buffer
	lines := NewLineSource(strings.NewReader("2\nin three files, not two\n\n"))
	v := NewView(&out, &log, NewConfirmer(lines, &out))

	asked := agent.Event{Kind: agent.Ask, Text: "which of these?", Options: []string{"the stat poll", "fsnotify"}}
	if got := v.Ask(context.Background(), asked); got != "fsnotify" {
		t.Errorf("the model was told %q, not the choice the number pointed at", got)
	}
	if got := v.Ask(context.Background(), agent.Event{Kind: agent.Ask, Text: "and how?"}); got != "in three files, not two" {
		t.Errorf("the model was told %q", got)
	}
	if got := v.Ask(context.Background(), agent.Event{Kind: agent.Ask, Text: "anything else?"}); got != "" {
		t.Errorf("an empty line answered %q", got)
	}
	for _, want := range []string{"which of these?", "1. the stat poll", "2. fsnotify", "your answer"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the terminal never saw %q:\n%s", want, out.String())
		}
	}
}

// a run reading a pipe has nobody at the keyboard: the question is said, and nothing is waited for
func TestNobodyIsWatchingThePipeToAnswer(t *testing.T) {
	var out, log bytes.Buffer
	v := NewView(&out, &log, nil)
	if got := v.Ask(context.Background(), agent.Event{Kind: agent.Ask, Text: "which of these?"}); got != "" {
		t.Errorf("a run nobody is watching answered %q", got)
	}
	if !strings.Contains(log.String(), "nobody here to answer") {
		t.Errorf("the question went unsaid: %q", log.String())
	}
}
