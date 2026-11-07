package agent

import (
	"context"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"strings"
	"testing"
)

func TestMaxTokensFitsTheServerContext(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "ok", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{MaxTurns: 1, MaxTokens: 4096, CtxSize: 4096}, &captureSink{})
	defer done()
	if err := a.Ask(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	got := int(stub.lastRequest(t)["max_tokens"].(float64))
	// the estimate now carries the system prompt and the tool specs too, so the room left is smaller
	if got >= 4096 || got < 2048 {
		t.Fatalf("max_tokens = %d, want it reduced below the 4096 context but still usable", got)
	}
}

func TestContextTooSmallFailsBeforeRequesting(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "ok", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{MaxTurns: 1, MaxTokens: 512, CtxSize: 120}, &captureSink{})
	defer done()

	err := a.Ask(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("err = %v, want a context-window error", err)
	}
	if len(stub.requests) != 0 {
		t.Fatalf("made %d requests, want none", len(stub.requests))
	}
}

func TestTheSafetyMarginSettingIsLeftFree(t *testing.T) {
	maxTokens := func(margin int) int {
		stub := &stubServer{scripts: []string{sse("", "ok", "", nil, "stop")}}
		a, done := newAgent(t, stub, Options{MaxTurns: 1, MaxTokens: 4096, CtxSize: 4096, SafetyMargin: margin}, &captureSink{})
		defer done()
		if err := a.Ask(context.Background(), "hi"); err != nil {
			t.Fatal(err)
		}
		return int(stub.lastRequest(t)["max_tokens"].(float64))
	}
	if small, large := maxTokens(256), maxTokens(1256); small-large != 1000 {
		t.Errorf("a 1000-token larger margin left %d fewer tokens for the reply", small-large)
	}
}

func TestEachThinkingModeSendsItsOwnSampling(t *testing.T) {
	temp := func(v float64) *float64 { return &v }
	opts := Options{MaxTurns: 1, MaxTokens: 64, Think: true,
		Thinking: llm.Sampling{Temperature: temp(0.6)}, NoThink: llm.Sampling{Temperature: temp(0.7)}}
	stub := &stubServer{scripts: []string{sse("", "ok", "", nil, "stop"), sse("", "ok", "", nil, "stop")}}
	a, done := newAgent(t, stub, opts, &captureSink{})
	defer done()

	if err := a.Ask(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	a.SetThink(false)
	if err := a.Ask(context.Background(), "hi again"); err != nil {
		t.Fatal(err)
	}
	if got := stub.requests[0]["temperature"]; got != 0.6 {
		t.Errorf("thinking request temperature = %v, want 0.6", got)
	}
	if got := stub.requests[1]["temperature"]; got != 0.7 {
		t.Errorf("no-think request temperature = %v, want 0.7", got)
	}
}

func TestNoSamplingSettingLeavesTheServerDefaults(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "ok", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{MaxTurns: 1, MaxTokens: 64}, &captureSink{})
	defer done()
	if err := a.Ask(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"temperature", "top_p", "top_k", "min_p", "presence_penalty"} {
		if _, sent := stub.lastRequest(t)[key]; sent {
			t.Errorf("%s was sent though nothing set it", key)
		}
	}
}
