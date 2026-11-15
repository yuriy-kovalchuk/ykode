package agent

import "testing"

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
	// the window minus the reserve is the ceiling; the prompt in front of it eats part of that room.
	// The reserve is the margin asked for, but never more than a quarter of the window
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

// thinking is billed inside the reply, so its budget is capped to leave the answer room of its own
func TestAThinkingBudgetNeverEatsTheAnswer(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{ThinkingLevel: "xhigh", CtxSize: 4000, SafetyMargin: 256}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}

	body := stub.lastRequest(t)
	budget, ok := body["thinking_budget_tokens"].(float64)
	if !ok {
		t.Fatal("a deep level must ask the server to cap the thinking phase")
	}
	cap, ok := body["max_tokens"].(float64)
	if !ok {
		t.Fatal("the reply still needs its own cap")
	}
	if cap-budget < 1024 {
		t.Errorf("a %v token reply leaves the answer %v tokens after %v of thinking", cap, cap-budget, budget)
	}
}

func TestAMediumThinkingBudgetReachesTheServer(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{ThinkingLevel: "medium", CtxSize: 16000, SafetyMargin: 256}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}

	if got := stub.lastRequest(t)["thinking_budget_tokens"]; got != float64(8192) {
		t.Errorf("thinking_budget_tokens = %v, want the 8192 a medium level asks for", got)
	}
}

func TestThinkingOffTurnsThePhaseOffWithoutNamingABudget(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{ThinkingLevel: "off", CtxSize: 8000}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}

	body := stub.lastRequest(t)
	kwargs, ok := body["chat_template_kwargs"].(map[string]any)
	if !ok || kwargs["enable_thinking"] != false {
		t.Errorf("chat_template_kwargs = %v, want enable_thinking false", body["chat_template_kwargs"])
	}
	if got, present := body["thinking_budget_tokens"]; present {
		t.Errorf("thinking_budget_tokens = %v with thinking off: the field must be left out", got)
	}
}

// an omitted budget would let thinking eat the whole reply
func TestASmallReplyStillCapsTheThinking(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{ThinkingLevel: "medium", MaxTokens: 512, CtxSize: 16000}, &captureSink{})
	defer done()
	if err := a.Ask(t.Context(), "say something"); err != nil {
		t.Fatal(err)
	}

	if got := stub.lastRequest(t)["thinking_budget_tokens"]; got != float64(256) {
		t.Errorf("thinking_budget_tokens = %v, want half of the 512 token reply", got)
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
}

// the fold mark is the last moment a fold still fits, not a share of the window: a fixed share folded
// a large window's whole results away while a fifth of it was still unused
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
