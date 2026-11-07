package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestEveryCommandAnswersSomething(t *testing.T) {
	a := New(nil, nil, "m", Options{}, nil)
	for _, line := range []string{"/help", "/usage", "/reset", "/sideways"} {
		text, quit, err := a.Command(context.Background(), line)
		if quit || err != nil {
			t.Fatalf("%s: quit=%v err=%v", line, quit, err)
		}
		if text == "" {
			t.Errorf("%s answered nothing", line)
		}
	}
}

func TestHelpListsEveryCommand(t *testing.T) {
	for _, want := range []string{"/help", "/usage", "/reset", "/exit", "/undo", "/history", "/compact",
		"/ctx", "/tools", "/model", "/think"} {
		if !strings.Contains(Help, want) {
			t.Errorf("help is missing %q:\n%s", want, Help)
		}
	}
}

func TestExitStopsTheSession(t *testing.T) {
	a := New(nil, nil, "m", Options{}, nil)
	if _, quit, _ := a.Command(context.Background(), "/exit"); !quit {
		t.Error("/exit did not end the session")
	}
	if _, quit, _ := a.Command(context.Background(), "/help"); quit {
		t.Error("/help ended the session")
	}
}

// countingUndo stands in for tools.UndoStack, which its own tests cover.
type countingUndo struct{ undone int }

func (c *countingUndo) Undo(bool) (string, error) { c.undone++; return "put back src/a.go", nil }
func (c *countingUndo) Empty() bool               { return c.undone > 0 }

func TestUndoTravelsWithTheAgent(t *testing.T) {
	undo := &countingUndo{}
	a := New(nil, nil, "m", Options{Undo: undo}, nil)

	text, _, err := a.Command(context.Background(), "/undo")
	if err != nil {
		t.Fatal(err)
	}
	if undo.undone != 1 {
		t.Errorf("/undo reached the stack %d times", undo.undone)
	}
	if !strings.Contains(text, "put back src/a.go") {
		t.Errorf("the undo said nothing useful: %q", text)
	}

	text, _, err = a.Command(context.Background(), "/undo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "nothing to undo") {
		t.Errorf("an empty stack should say so: %q", text)
	}
}

func TestTheContextCommandSaysWhatTheWindowHolds(t *testing.T) {
	a, done := newAgent(t, &stubServer{}, Options{MaxTurns: 2, CtxSize: 8000, MaxResult: 4096}, &captureSink{})
	defer done()
	text, quit, err := a.Command(context.Background(), "/ctx")
	if quit || err != nil {
		t.Fatalf("quit=%v err=%v", quit, err)
	}
	for _, want := range []string{"window 8000", "tool descriptions", "capped at 4096 bytes"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q missing from:\n%s", want, text)
		}
	}
	if strings.Contains(text, "speculative") {
		t.Errorf("no draft model ran, so speculation should not be mentioned:\n%s", text)
	}
}

func TestTheToolsCommandPricesEveryDescription(t *testing.T) {
	a, done := newAgent(t, &stubServer{}, Options{MaxTurns: 2, CtxSize: 8000}, &captureSink{})
	defer done()
	text, _, err := a.Command(context.Background(), "/tools")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"find_files", "read_file", "write_file", "3 tools", "bytes in every request"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q missing from:\n%s", want, text)
		}
	}
}

func TestTheModelAndThinkCommandsChangeTheRun(t *testing.T) {
	a, done := newAgent(t, &stubServer{}, Options{MaxTurns: 2, CtxSize: 8000, Think: true}, &captureSink{})
	defer done()
	if text, _, _ := a.Command(context.Background(), "/model"); !strings.Contains(text, "test-model") {
		t.Errorf("/model did not name the model: %s", text)
	}
	if _, _, err := a.Command(context.Background(), "/model other-model"); err != nil {
		t.Fatal(err)
	}
	if a.Model() != "other-model" {
		t.Errorf("the run still asks for %s", a.Model())
	}
	if _, _, err := a.Command(context.Background(), "/think off"); err != nil {
		t.Fatal(err)
	}
	if a.Thinking() {
		t.Error("/think off left thinking on")
	}
	if _, _, err := a.Command(context.Background(), "/think maybe"); err == nil {
		t.Error("/think accepted something that is not on or off")
	}
}

func TestCommandsThatChangeTheRunWaitForTheTurn(t *testing.T) {
	// a settings command waits even when it only looks: the turn in flight is its answer's context
	for _, line := range []string{"/model x", "/model", "/think off", "/reset", "/compact", "/undo"} {
		if CommandIsSafeDuringATurn(line) {
			t.Errorf("%s would fight the turn in flight", line)
		}
	}
	for _, line := range []string{"/ctx", "/tools", "/usage", "/history"} {
		if !CommandIsSafeDuringATurn(line) {
			t.Errorf("%s only reads and should answer at once", line)
		}
	}
}

func TestThinkingOffReachesTheNextRequest(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "first", "", nil, "stop"),
		sse("", "second", "", nil, "stop"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 2, MaxTokens: 128, CtxSize: 8000, Think: true}, &captureSink{})
	defer done()
	if err := a.Ask(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	if kwargs, _ := stub.lastRequest(t)["chat_template_kwargs"].(map[string]any); kwargs["enable_thinking"] == false {
		t.Fatal("thinking was on, but the request turned it off")
	}
	if _, _, err := a.Command(context.Background(), "/think off"); err != nil {
		t.Fatal(err)
	}
	if err := a.Ask(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}
	kwargs, ok := stub.lastRequest(t)["chat_template_kwargs"].(map[string]any)
	if !ok || kwargs["enable_thinking"] != false {
		t.Errorf(`/think off did not reach the request: %v`, stub.lastRequest(t)["chat_template_kwargs"])
	}
}

func TestCompactStopsWhenTheCallerCancels(t *testing.T) {
	dir := t.TempDir()
	read := func(i int) string {
		name := writeLines(t, dir, fmt.Sprintf("file%d.txt", i), 20)
		return sse("", "", "read_file", []string{`{"path":"` + name + `"}`}, "tool_calls")
	}
	stub := &stubServer{scripts: []string{read(0), read(1), read(2), sse("", "all read", "", nil, "stop")}}
	a, done := newStoringAgent(t, stub, Options{MaxTurns: 6, MaxTokens: 64, CtxSize: 40000}, &captureSink{decision: Allow}, dir)
	defer done()
	if err := a.Ask(context.Background(), "read them"); err != nil {
		t.Fatal(err)
	}
	asked := len(stub.requests)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	text, _, _ := a.Command(ctx, "/compact")

	if len(stub.requests) != asked {
		t.Error("a cancelled /compact still asked the model for a summary")
	}
	if strings.Contains(text, "tool results shortened") {
		t.Errorf("/compact reported shortening whatever it did: %q", text)
	}
}
