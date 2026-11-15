package agent

import (
	"context"
	"testing"
)

// toolRounds scripts n distinct read_file calls and then a plain answer.
func toolRounds(n int) []string {
	var scripts []string
	for i := 0; i < n; i++ {
		scripts = append(scripts, sse("", "", "read_file", []string{`{"path":"f` + string(rune('a'+i)) + `.go"}`}, "tool_calls"))
	}
	return append(scripts, sse("", "done", "", nil, "stop"))
}

// with no limit the run goes on until the model answers, and nothing warns it of a limit
func TestNoLimitRunsUntilTheAnswer(t *testing.T) {
	stub := &stubServer{scripts: toolRounds(6)}
	a, done := newAgent(t, stub, Options{MaxTurns: 0, MaxTokens: 64}, &captureSink{})
	defer done()
	if err := a.Ask(context.Background(), "look around"); err != nil {
		t.Fatal(err)
	}
	if notes := asksSent(a); len(notes) != 0 {
		t.Errorf("an unlimited run was sent %q", notes)
	}
	if last := a.history[len(a.history)-1]; last.Content != "done" {
		t.Errorf("the run ended on %q, want the model's own answer", last.Content)
	}
}
