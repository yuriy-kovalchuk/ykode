package cli

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// A writer that fails after the first line, which is what a disk that filled up mid-answer, or a
// stream a person hung up on, looks like from here.
type failingWriter struct {
	after int
	n     int
}

func (f *failingWriter) Write(b []byte) (int, error) {
	f.n++
	if f.n > f.after {
		return 0, errors.New("no space left on device")
	}
	return len(b), nil
}

// The answer goes to stdout and the run is judged by its exit code, so an answer that was produced and
// never delivered is a failed run, not a successful one that happened to say nothing.
func TestAWriteThatFailsIsKeptForTheRunToReport(t *testing.T) {
	out, log := &failingWriter{after: 1}, &failingWriter{after: 100}
	v := NewView(out, log)
	v.Emit(agent.Event{Kind: agent.Answer, Text: "the first line"})
	if err := v.Err(); err != nil {
		t.Fatalf("the answer that did land was reported as a failure: %v", err)
	}
	v.Emit(agent.Event{Kind: agent.Answer, Text: "the line that did not"})
	err := v.Err()
	if err == nil {
		t.Fatal("a write that failed was lost: the run would exit as if the answer had been delivered")
	}
	if !strings.Contains(err.Error(), "no space left") {
		t.Errorf("the failure kept is not the one that happened: %v", err)
	}
}

// the log stream counts too: the tool lines are what a script reads for what the run did
func TestAFailedLogWriteIsKeptAsWell(t *testing.T) {
	out, log := &failingWriter{after: 100}, &failingWriter{after: 0}
	v := NewView(out, log)
	v.Emit(agent.Event{Kind: agent.Notice, Text: "something the run did"})
	if v.Err() == nil {
		t.Error("a log line that did not land was lost")
	}
}

// everything reaching its stream is not an error, and says so
func TestAPlainRunReportsNoWriteFailure(t *testing.T) {
	v := NewView(io.Discard, io.Discard)
	v.Emit(agent.Event{Kind: agent.Answer, Text: "fine"})
	v.Emit(agent.Event{Kind: agent.TurnEnd, Stats: agent.Stats{PromptTokens: 4, OutputTokens: 2}})
	if err := v.Err(); err != nil {
		t.Errorf("a run whose every write landed reported %v", err)
	}
}
