package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

func TestThinkingAndAnswerGoToDifferentStreams(t *testing.T) {
	var out, log bytes.Buffer
	r := NewRenderer(&out, &log)

	r.Reasoning("pondering ")
	r.Reasoning("the problem")
	r.Content("the answer")
	r.TurnDone(agent.Stats{
		PromptTokens:  312,
		CachedTokens:  12,
		OutputTokens:  90,
		PrefillTokSec: 454,
		DecodeTokSec:  35,
		FinishReason:  "stop",
	})

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
	r := NewRenderer(&out, &log)
	r.colored = true

	// A turn interrupted before any answer must still reset the terminal.
	r.Reasoning("half a thought")
	r.TurnDone(agent.Stats{})

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
	r := NewRenderer(&out, &log)
	r.Content("answer")
	r.TurnDone(agent.Stats{})

	if log.String() != "" {
		t.Fatalf("log = %q, want no timing line", log.String())
	}
	if out.String() != "answer\n" {
		t.Fatalf("answer stream = %q", out.String())
	}
}
