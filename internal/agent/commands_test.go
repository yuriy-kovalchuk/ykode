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

func TestExitStopsTheSession(t *testing.T) {
	a := New(nil, nil, "m", Options{}, nil)
	if _, quit, _ := a.Command(context.Background(), "/exit"); !quit {
		t.Error("/exit did not end the session")
	}
	if _, quit, _ := a.Command(context.Background(), "/help"); quit {
		t.Error("/help ended the session")
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
	for _, want := range []string{"read_file", "write_file", "2 tools", "bytes in every request"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q missing from:\n%s", want, text)
		}
	}
}

func TestTheModelAndThinkCommandsChangeTheRun(t *testing.T) {
	a, done := newAgent(t, &stubServer{}, Options{MaxTurns: 2, CtxSize: 8000, ThinkingLevel: "medium"}, &captureSink{})
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
	if a.ThinkingLevel() != "off" {
		t.Errorf("/think off left the level at %q", a.ThinkingLevel())
	}
	if _, _, err := a.Command(context.Background(), "/think maybe"); err == nil {
		t.Error("/think accepted something that is not a level")
	}
}

// the same word pi uses: a level, not a switch
func TestTheThinkCommandTakesALevel(t *testing.T) {
	a, done := newAgent(t, &stubServer{}, Options{MaxTurns: 2, CtxSize: 8000, ThinkingLevel: "medium"}, &captureSink{})
	defer done()
	if _, _, err := a.Command(context.Background(), "/think low"); err != nil {
		t.Fatal(err)
	}
	if a.ThinkingLevel() != "low" {
		t.Errorf("level = %q, want low", a.ThinkingLevel())
	}
	if text, _, err := a.Command(context.Background(), "/think"); err != nil || !strings.Contains(text, "low") {
		t.Errorf("/think on its own should report the level: %q err=%v", text, err)
	}
	if _, _, err := a.Command(context.Background(), "/think enormous"); err == nil || !strings.Contains(err.Error(), "xhigh") {
		t.Errorf("/think enormous: %v, want an error naming the levels", err)
	}
	if _, _, err := a.Command(context.Background(), "/think on"); err != nil {
		t.Fatal(err)
	}
	if a.ThinkingLevel() != "medium" {
		t.Errorf("/think on set %q, want the default depth back", a.ThinkingLevel())
	}
}

func TestCommandsThatChangeTheRunWaitForTheTurn(t *testing.T) {
	// a settings command waits even when it only looks: the turn in flight is its answer's context
	for _, line := range []string{"/model x", "/model", "/think off", "/reset", "/compact"} {
		if CommandIsSafeDuringATurn(line) {
			t.Errorf("%s would fight the turn in flight", line)
		}
	}
	for _, line := range []string{"/ctx", "/tools", "/usage"} {
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
	a, done := newAgent(t, stub, Options{MaxTurns: 2, MaxTokens: 128, CtxSize: 8000, ThinkingLevel: "medium"}, &captureSink{})
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
	a, done := newAgentIn(t, stub, Options{MaxTurns: 6, MaxTokens: 64, CtxSize: 40000}, &captureSink{decision: Allow}, dir)
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

// the list a front end completes from and the list /help prints are the same list, and it says
// which commands wait for the turn in flight
func TestTheCommandListIsWhatHelpAndSafetyUse(t *testing.T) {
	a := New(nil, nil, "m", Options{}, nil)
	help, _, err := a.Command(context.Background(), "/help")
	if err != nil {
		t.Fatal(err)
	}
	list := Commands()
	if len(list) == 0 {
		t.Fatal("no commands to complete")
	}
	for _, c := range list {
		if c.Note == "" {
			t.Errorf("%s says what it does not do", c.Name)
		}
		// every name the command answers to is in the help and answered the same way
		for _, spelled := range c.Spellings() {
			if !strings.Contains(help, spelled) {
				t.Errorf("%s is answered as %s, which /help does not mention:\n%s", c.Name, spelled, help)
			}
			if c.Waits == CommandIsSafeDuringATurn(spelled) {
				t.Errorf("%s waits=%v but %s is answered differently", c.Name, c.Waits, spelled)
			}
		}
	}
	// nothing the model of the list hides: every command the switch answers is in it
	for _, name := range []string{"/help", "/usage", "/reset", "/exit", "/quit", "/compact",
		"/ctx", "/tools", "/model", "/think"} {
		if !hasCommand(list, name) {
			t.Errorf("%s is not in the command list", name)
		}
	}
}

func hasCommand(list []CommandSpec, name string) bool {
	for _, c := range list {
		for _, spelled := range c.Spellings() {
			if spelled == name {
				return true
			}
		}
	}
	return false
}

// starting over is announced, so a screen can stop reporting the numbers of a conversation that is gone
func TestStartingOverSaysTheConversationIsGone(t *testing.T) {
	sink := &captureSink{}
	a := New(nil, nil, "m", Options{}, sink)
	if _, _, err := a.Command(context.Background(), "/reset"); err != nil {
		t.Fatal(err)
	}
	for _, ev := range sink.events {
		if ev.Kind == Cleared {
			return
		}
	}
	t.Error("clearing the conversation was not reported to the front end")
}
