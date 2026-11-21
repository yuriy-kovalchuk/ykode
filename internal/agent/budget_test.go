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

// what the settings show for a level is what the request carries, cut to the room the window has left
func TestTheBudgetShownForALevelIsTheOneTheServerGets(t *testing.T) {
	a := newForHistory(Options{ThinkingLevel: "xhigh", CtxSize: 3000, SafetyMargin: 256})
	a.add(llm.Message{Role: "user", Content: "say something"})
	req, err := a.request()
	if err != nil {
		t.Fatal(err)
	}
	if shown := a.ThinkingBudget("xhigh"); req.ThinkingBudget != shown {
		t.Errorf("the request asked for %d but %d was shown", req.ThinkingBudget, shown)
	}
	if low, xhigh := a.ThinkingBudget("low"), a.ThinkingBudget("xhigh"); xhigh >= 32768 || low != xhigh {
		t.Errorf("a 3000 token window gave xhigh %d and low %d: both should be cut to the same room", xhigh, low)
	}
	if got := a.ThinkingBudget("minimal"); got != 1024 {
		t.Errorf("minimal fits the window whole, yet shows %d", got)
	}
}
