package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
)

// refusing answers like radiance: a word outside the set is a 400 that names the set
func refusing(takes ...string) func(map[string]any) string {
	return func(req map[string]any) string {
		word, _ := req["reasoning_effort"].(string)
		if word == "" || slices.Contains(takes, word) {
			return ""
		}
		return fmt.Sprintf(`{"error":{"message":"reasoning_effort: '%s' is not a level this model's chat template takes",`+
			`"param":"reasoning_effort","supported":%s}}`, word, jsonList(takes))
	}
}

func jsonList(words []string) string {
	raw, _ := json.Marshal(words)
	return string(raw)
}

func effortsSent(stub *stubServer) []any {
	sent := make([]any, 0, len(stub.requests))
	for _, req := range stub.requests {
		sent = append(sent, req["reasoning_effort"])
	}
	return sent
}

// a level reaches the server as its effort word, so the depth is the one asked for and not the server's
func TestTheThinkingLevelReachesTheServerAsReasoningEffort(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{ThinkingLevel: "high", CtxSize: 16000}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}
	if got := stub.lastRequest(t)["reasoning_effort"]; got != "high" {
		t.Errorf("reasoning_effort = %v, want high", got)
	}
}

// a word the template turns down is asked again with the nearest it named, and that is kept for the model
func TestARefusedEffortIsAskedAgainWithTheNearestTheModelTakes(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")},
		refuse: refusing("none", "minimal", "low", "medium", "xhigh")}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{ThinkingLevel: "max", CtxSize: 16000}, sink)
	defer done()
	for range 2 {
		if err := a.Ask(t.Context(), "say something"); err != nil {
			t.Fatal(err)
		}
	}
	if got := fmt.Sprint(effortsSent(stub)); got != "[max xhigh xhigh]" {
		t.Errorf("efforts sent = %s, want max refused once and xhigh from then on", got)
	}
	if !sink.noticed(`does not take reasoning effort "max": asking with "xhigh"`) {
		t.Error("the switch to another word was not said")
	}
}

// with no list in the refusal the ladder is walked, deeper first, one word ruled out per refusal
func TestARefusalWithNoListWalksTheLadder(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	stub.refuse = func(req map[string]any) string {
		if word := req["reasoning_effort"]; word == "high" || word == "xhigh" {
			return `{"error":{"message":"reasoning_effort \"` + word.(string) + `\" is not supported by the chat template"}}`
		}
		return ""
	}
	a, done := newAgent(t, stub, Options{ThinkingLevel: "high", CtxSize: 16000}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(effortsSent(stub)); got != "[high xhigh medium]" {
		t.Errorf("efforts sent = %s, want high, then xhigh, then medium", got)
	}
}

// a template that takes no word at all ends with the field left out, not with a turn that never stops
func TestAModelThatTakesNoEffortWordIsAskedWithout(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	stub.refuse = func(req map[string]any) string {
		if _, sent := req["reasoning_effort"]; sent {
			return `{"error":{"message":"reasoning_effort is not supported by this model"}}`
		}
		return ""
	}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{ThinkingLevel: "medium", CtxSize: 16000}, sink)
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}
	if _, sent := stub.lastRequest(t)["reasoning_effort"]; sent {
		t.Error("the last request still named an effort")
	}
	if len(stub.requests) > 8 {
		t.Errorf("%d requests: the ladder has 6 thinking words, so the walk must end", len(stub.requests))
	}
	if !sink.noticed("asking without one") {
		t.Error("dropping the field was not said")
	}
}

// off that a server refuses as none is left to enable_thinking alone, never turned into a thinking word
func TestARefusedNoneLeavesOffToEnableThinking(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}, refuse: refusing("low", "medium", "xhigh")}
	a, done := newAgent(t, stub, Options{ThinkingLevel: "off", CtxSize: 16000}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}
	last := stub.lastRequest(t)
	if _, sent := last["reasoning_effort"]; sent || len(stub.requests) != 2 {
		t.Errorf("after none was refused: %d requests, reasoning_effort = %v", len(stub.requests), last["reasoning_effort"])
	}
	if kwargs, _ := last["chat_template_kwargs"].(map[string]any); kwargs["enable_thinking"] != false {
		t.Errorf("chat_template_kwargs = %v, want enable_thinking false", last["chat_template_kwargs"])
	}
}

// a model switch starts from the level's own word: what one template refused says nothing of another
func TestWhatOneModelRefusedDoesNotFollowASwitch(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}, refuse: refusing("low", "medium", "xhigh")}
	a, done := newAgent(t, stub, Options{ThinkingLevel: "high", CtxSize: 16000}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}
	stub.refuse = nil
	a.SetModel("other-model")
	if err := a.Ask(context.Background(), "and again"); err != nil {
		t.Fatal(err)
	}
	if got := stub.lastRequest(t)["reasoning_effort"]; got != "high" {
		t.Errorf("the other model was sent %v, want its own high", got)
	}
}
