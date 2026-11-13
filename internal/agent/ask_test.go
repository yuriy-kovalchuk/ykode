package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// asksBack is a sink a model may put its own question to, and it remembers what it was asked.
type asksBack struct {
	captureSink
	answer    string // what the person types
	questions []Event
}

func (s *asksBack) Ask(_ context.Context, ev Event) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.questions = append(s.questions, ev)
	return s.answer
}

// the model asks in words, the person answers in words, and the answer arrives as the tool result
func TestTheModelAsksAndReadsTheAnswerBack(t *testing.T) {
	args := `{"question":"which way should the footer follow the branch?","options":["stat HEAD each 5s","a file watcher","leave it"]}`
	stub := &stubServer{scripts: []string{
		sse("", "", "ask_user", []string{args}, "tool_calls"),
		sse("", "the stat it is", "", nil, "stop"),
	}}
	sink := &asksBack{answer: "1"}
	a, stop := newAgent(t, stub, Options{MaxTokens: 128, CtxSize: 8000}, sink)
	defer stop()
	if err := a.Ask(context.Background(), "sort out the footer"); err != nil {
		t.Fatal(err)
	}

	if len(sink.questions) != 1 {
		t.Fatalf("the screen was asked %d times", len(sink.questions))
	}
	asked := sink.questions[0]
	if asked.Kind != Ask || asked.Text != "which way should the footer follow the branch?" {
		t.Errorf("the question on the screen: %+v", asked)
	}
	if len(asked.Options) != 3 || asked.Options[2] != "leave it" {
		t.Errorf("the choices did not reach the screen: %q", asked.Options)
	}

	// the second request carries what the person typed back, as the result of that call
	followed, err := json.Marshal(stub.requests[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(followed), "stat HEAD each 5s") {
		t.Errorf("the model never got the answer it asked for: %s", followed)
	}
}

// a bare question with no choices is still worth asking
func TestAQuestionWithNoChoicesStillAsks(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "ask_user", []string{`{"question":"which file should I start with?"}`}, "tool_calls"),
		sse("", "noted", "", nil, "stop"),
	}}
	sink := &asksBack{answer: "internal/tui/status.go"}
	a, stop := newAgent(t, stub, Options{MaxTokens: 128, CtxSize: 8000}, sink)
	defer stop()
	if err := a.Ask(context.Background(), "start somewhere"); err != nil {
		t.Fatal(err)
	}
	if len(sink.questions) != 1 || len(sink.questions[0].Options) != 0 {
		t.Fatalf("what reached the screen: %+v", sink.questions)
	}
	followed, err := json.Marshal(stub.requests[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(followed), "internal/tui/status.go") {
		t.Errorf("the words typed were not handed back: %s", followed)
	}
}

// a front end that cannot answer is not offered the tool: a call it cannot answer would only stall
func TestTheQuestionIsOfferedOnlyWhereSomeoneCanAnswer(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	offered := func(sink Sink) map[string]bool {
		names := map[string]bool{}
		for _, spec := range New(nil, tools.BuiltIn(root, dir, tools.Options{}), "m", Options{}, sink).Specs() {
			names[spec.Name] = true
		}
		return names
	}
	if offered(&captureSink{})["ask_user"] {
		t.Error("a sink that only watches was offered a question it cannot answer")
	}
	if !offered(&asksBack{})["ask_user"] {
		t.Error("a sink that can answer was not offered the question")
	}
}
