package agent

import (
	"context"
	"strings"
	"testing"
)

// toolRounds scripts n distinct find_files calls and then a plain answer.
func toolRounds(n int) []string {
	var scripts []string
	for i := 0; i < n; i++ {
		scripts = append(scripts, sse("", "", "find_files", []string{`{"pattern":"*","depth":` + string(rune('1'+i)) + `}`}, "tool_calls"))
	}
	return append(scripts, sse("", "done", "", nil, "stop"))
}

// a model nearing the limit is told once, while it still has rounds to finish in
func TestTheModelIsWarnedBeforeTheRoundsRunOut(t *testing.T) {
	stub := &stubServer{scripts: toolRounds(4)}
	a, done := newAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 64, NoVerify: true}, &captureSink{})
	defer done()
	if err := a.Ask(context.Background(), "look around"); err != nil {
		t.Fatal(err)
	}
	var warnings []string
	for _, note := range asksSent(a) {
		if strings.Contains(note, "tool rounds this request gets") {
			warnings = append(warnings, note)
		}
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "used 4 of the 5") {
		t.Fatalf("warnings sent: %q", warnings)
	}
	if last := a.history[len(a.history)-1]; last.Content != "done" {
		t.Errorf("the run ended on %q, want the model's own answer", last.Content)
	}
}

// with no limit the run goes on until the model answers, and nothing warns it of a limit
func TestNoLimitRunsUntilTheAnswer(t *testing.T) {
	stub := &stubServer{scripts: toolRounds(6)}
	a, done := newAgent(t, stub, Options{MaxTurns: 0, MaxTokens: 64, NoVerify: true}, &captureSink{})
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

func TestTheWarningComesInTheLastFifth(t *testing.T) {
	for _, tc := range []struct{ max, want int }{
		{0, 0}, {4, 0}, {5, 4}, {50, 40}, {100, 80},
	} {
		if got := wrapUpTurn(tc.max); got != tc.want {
			t.Errorf("wrapUpTurn(%d) = %d, want %d", tc.max, got, tc.want)
		}
	}
}
