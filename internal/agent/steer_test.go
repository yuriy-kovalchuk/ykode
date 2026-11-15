package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// steerSink adds an ask to the run as soon as it sees the event it waits for, which is the
// mid-turn moment a front end would be typing at.
type steerSink struct {
	captureSink
	wait  EventKind
	ask   string
	agent *Agent
	gave  bool
}

func (s *steerSink) Emit(ev Event) Decision {
	if !s.gave && ev.Kind == s.wait {
		s.gave = true
		s.agent.Steer(s.ask)
	}
	return s.captureSink.Emit(ev)
}

// what was typed while a turn ran reaches the model at the next round-trip, not after the whole
// turn: the round-trip already in flight and the tools already running are not interrupted
func TestASteeredAskJoinsTheNextRoundTrip(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"seeded.txt"}`}, "tool_calls"),
		sse("", "both files are there", "", nil, "stop"),
	}}
	sink := &steerSink{wait: ToolEnd, ask: "and the tests too"}
	a, done := newAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 512}, sink)
	defer done()
	sink.agent = a

	if err := a.Ask(context.Background(), "what files are here?"); err != nil {
		t.Fatal(err)
	}
	second := stub.requests[1]["messages"].([]any)
	if !hasVoice(second, "user", "and the tests too") {
		t.Fatalf("the next round-trip never got the ask:\n%v", second)
	}
	if !steered(sink, "and the tests too") {
		t.Errorf("the run never said it took the ask: %v", sink.kinds())
	}
}

// an ask that was typed but never reached the model is handed back rather than lost
func TestAnAskWaitingWhenTheRunEndedIsHandedBack(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "one file", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 512}, &captureSink{})
	defer done()

	a.Steer("and the tests too")
	if got := a.Waiting(); len(got) != 1 || got[0] != "and the tests too" {
		t.Fatalf("waiting = %v, want the ask shown until it is delivered", got)
	}
	ask, ok := a.NextWaiting()
	if !ok || ask != "and the tests too" {
		t.Fatalf("next waiting = %q, ok=%v, want the ask to start as an ordinary prompt", ask, ok)
	}
	if _, ok := a.NextWaiting(); ok {
		t.Errorf("the same ask was handed over twice")
	}
}

// starting over forgets what was waiting too: it would otherwise land in an empty conversation
func TestStartingOverDropsWhatWasWaiting(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "one file", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 512}, &captureSink{})
	defer done()

	a.Steer("and the tests too")
	a.Reset()
	if got := a.Waiting(); len(got) != 0 {
		t.Fatalf("waiting after a reset = %v, want nothing", got)
	}
}

// two asks typed in a row arrive in the order they were typed
func TestAsksAreTakenInTheOrderTheyWereTyped(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"seeded.txt"}`}, "tool_calls"),
		sse("", "done", "", nil, "stop"),
	}}
	sink := &steerSink{wait: ToolEnd, ask: "first", agent: nil}
	a, done := newAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 512}, sink)
	defer done()
	sink.agent = a
	sink.gave = false

	if err := a.Ask(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	a.Steer("second")
	messages := stub.requests[1]["messages"].([]any)
	if !hasVoice(messages, "user", "first") {
		t.Fatalf("the first ask is missing:\n%v", messages)
	}
	if got := a.Waiting(); len(got) != 1 || got[0] != "second" {
		t.Fatalf("waiting = %v, want the ask typed after the run to stay", got)
	}
}

// steeredText says whether the run reported taking an ask.
func steered(sink *steerSink, text string) bool {
	for _, ev := range sink.events {
		if ev.Kind == Steered && strings.Contains(ev.Text, text) {
			return true
		}
	}
	return false
}

// hasVoice says whether a request carries a message in that voice holding that text.
func hasVoice(messages []any, role, text string) bool {
	for _, raw := range messages {
		m := raw.(map[string]any)
		if m["role"] == role && strings.Contains(fmt.Sprint(m["content"]), text) {
			return true
		}
	}
	return false
}
