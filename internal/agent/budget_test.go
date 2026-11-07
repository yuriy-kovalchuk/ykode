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
	// the window minus the margin is the ceiling; the prompt in front of it eats part of that room
	if maxTokens > 8000-4096 {
		t.Errorf("max_tokens = %v: want at most the window minus the margin", maxTokens)
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
