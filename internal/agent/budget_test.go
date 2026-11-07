package agent

import (
	"context"
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
