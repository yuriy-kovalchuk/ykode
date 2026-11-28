package agent

import (
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// a reply with no cap of its own is still bounded by the room the context window has left
func TestANoCapReplyGetsTheRoomTheWindowHasLeft(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{MaxTokens: 0, CtxSize: 8000, SafetyMargin: 4096}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}

	body := stub.lastRequest(t)
	maxTokens, ok := body["max_tokens"].(float64)
	if !ok {
		t.Fatal("a known window must still bound the reply")
	}
	// the window minus the reserve is the ceiling, and the reserve is never over a quarter of it
	if maxTokens > 8000-8000/4 {
		t.Errorf("max_tokens = %v: want at most the window minus the reserve", maxTokens)
	}
	if maxTokens < 1024 {
		t.Errorf("max_tokens = %v: what is left after the prompt should still be a usable reply", maxTokens)
	}
}

// with no cap asked for and no window known, nothing is sent: the server decides where the reply ends
func TestAnUnknownWindowAsksForNoCap(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{MaxTokens: 0, CtxSize: 0}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}

	if got, ok := stub.lastRequest(t)["max_tokens"]; ok {
		t.Errorf("max_tokens = %v, want the field left out", got)
	}
}

// thinking is billed inside the reply, so its cap is cut to leave the answer room of its own
func TestAThinkingCapNeverEatsTheAnswer(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{ThinkingBudget: 32768, CtxSize: 4000, SafetyMargin: 256}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}

	body := stub.lastRequest(t)
	budget, ok := body["reasoning_budget_tokens"].(float64)
	if !ok || body["thinking_budget_tokens"] != budget {
		t.Fatalf("the cap must go under both names: %v and %v", body["reasoning_budget_tokens"], body["thinking_budget_tokens"])
	}
	cap, ok := body["max_tokens"].(float64)
	if !ok {
		t.Fatal("the reply still needs its own cap")
	}
	if cap-budget < 1024 {
		t.Errorf("a %v token reply leaves the answer %v tokens after %v of thinking", cap, cap-budget, budget)
	}
}

// the cap is a setting of its own: the level says how deeply, the cap how many tokens
func TestTheThinkingCapIsNotTheLevel(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{ThinkingLevel: "low", ThinkingBudget: 6000, CtxSize: 16000, SafetyMargin: 256},
		&captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}

	body := stub.lastRequest(t)
	if body["reasoning_effort"] != "low" || body["reasoning_budget_tokens"] != float64(6000) {
		t.Errorf("reasoning_effort = %v, cap = %v: want low and the 6000 asked for",
			body["reasoning_effort"], body["reasoning_budget_tokens"])
	}
}

// no cap of its own still leaves the answer its room, or thinking can eat the reply and return nothing
func TestNoThinkingCapStillLeavesTheAnswerRoom(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{ThinkingLevel: "max", MaxTokens: 8000, CtxSize: 32000}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}
	if got := stub.lastRequest(t)["reasoning_budget_tokens"]; got != float64(8000-1024) {
		t.Errorf("reasoning_budget_tokens = %v, want the 8000 token reply less the answer's 1024", got)
	}
}

// with no reply size known there is nothing to cut against, so no cap is sent at all
func TestNoThinkingCapAndNoWindowSendsNone(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{ThinkingLevel: "high"}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}
	body := stub.lastRequest(t)
	for _, field := range []string{"reasoning_budget_tokens", "thinking_budget_tokens"} {
		if got, present := body[field]; present {
			t.Errorf("%s = %v with no window and no cap: the field must be left out", field, got)
		}
	}
}

func TestThinkingOffTurnsThePhaseOffWithoutNamingABudget(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{ThinkingLevel: "off", ThinkingBudget: 4096, CtxSize: 8000}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}

	body := stub.lastRequest(t)
	kwargs, ok := body["chat_template_kwargs"].(map[string]any)
	if !ok || kwargs["enable_thinking"] != false {
		t.Errorf("chat_template_kwargs = %v, want enable_thinking false", body["chat_template_kwargs"])
	}
	if body["reasoning_effort"] != "none" {
		t.Errorf("reasoning_effort = %v, want none, which every engine reads as no thinking", body["reasoning_effort"])
	}
	for _, field := range []string{"reasoning_budget_tokens", "thinking_budget_tokens"} {
		if got, present := body[field]; present {
			t.Errorf("%s = %v with thinking off: the field must be left out", field, got)
		}
	}
}

// an omitted budget would let thinking eat the whole reply
func TestASmallReplyStillCapsTheThinking(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{ThinkingBudget: 8192, MaxTokens: 512, CtxSize: 16000}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}

	if got := stub.lastRequest(t)["reasoning_budget_tokens"]; got != float64(256) {
		t.Errorf("reasoning_budget_tokens = %v, want half of the 512 token reply", got)
	}
}

// every request asks llama.cpp for its speeds in each chunk, so the screen can show them live
func TestEveryRequestAsksForLiveTimings(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{CtxSize: 8000}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}
	if got := stub.lastRequest(t)["timings_per_token"]; got != true {
		t.Errorf("timings_per_token = %v, want true", got)
	}
	if got := stub.lastRequest(t)["return_progress"]; got != true {
		t.Errorf("return_progress = %v, want true, so a long prompt says how far it has been read", got)
	}
}

// the fold mark is the last moment a fold still fits, not a share of the window
func TestTheFoldMarkIsTheLastMomentAFoldFits(t *testing.T) {
	for _, tc := range []struct{ ctx, margin int }{{4096, 1024}, {32768, 4096}, {131072, 4096}} {
		a := newForHistory(Options{CtxSize: tc.ctx, SafetyMargin: tc.margin})
		if asked := a.pressureMark(); asked > a.usable()-a.compactRoom() {
			t.Errorf("at a %d window the fold is asked at %d, past the last request that fits (%d)",
				tc.ctx, asked, a.usable()-a.compactRoom())
		}
		if asked := a.pressureMark(); asked > a.ceiling() {
			t.Errorf("at a %d window the fold waits until after the drop mark (%d)", tc.ctx, a.ceiling())
		}
		if tc.ctx >= 32768 && a.pressureMark() <= tc.ctx*3/4 {
			t.Errorf("at a %d window the fold asks at %d, no later than the old three quarters of the window",
				tc.ctx, a.pressureMark())
		}
	}
}

// a request refused for room names the share held for the answer, or a part-empty window reads as a miscount
func TestAConversationOutOfRoomNamesTheRoomHeldForTheAnswer(t *testing.T) {
	a := newForHistory(Options{CtxSize: 2000, SafetyMargin: 400})
	a.add(llm.Message{Role: "user", Content: strings.Repeat("word ", 1600)})
	_, err := a.request()
	if err == nil {
		t.Fatal("a conversation past the window was sent")
	}
	if !strings.Contains(err.Error(), "400 of it held back for the answer") || !strings.Contains(err.Error(), "safety_margin") {
		t.Errorf("the refusal does not name the reserve: %v", err)
	}
}

// what the settings show for the cap is what the request carries, cut to the room the window has left
func TestTheCapShownIsTheOneTheServerGets(t *testing.T) {
	a := newForHistory(Options{ThinkingBudget: 32768, CtxSize: 3000, SafetyMargin: 256})
	a.add(llm.Message{Role: "user", Content: "say something"})
	req, err := a.request()
	if err != nil {
		t.Fatal(err)
	}
	if shown := a.ThinkingBudget(); req.ReasoningBudget != shown || shown >= 32768 {
		t.Errorf("the request asked for %d but %d was shown, from a 32768 cap in a 3000 token window",
			req.ReasoningBudget, shown)
	}
}
