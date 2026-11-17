package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// twoCalls builds one assistant turn with two tool calls: several things asked at once
func twoCalls(toolA, argsA, toolB, argsB string) string {
	var b strings.Builder
	raw, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"index": 0, "delta": map[string]any{
		"tool_calls": []map[string]any{
			{"index": 0, "id": "callA", "type": "function", "function": map[string]any{"name": toolA, "arguments": argsA}},
			{"index": 1, "id": "callB", "type": "function", "function": map[string]any{"name": toolB, "arguments": argsB}},
		},
	}, "finish_reason": "tool_calls"}}})
	fmt.Fprintf(&b, "data: %s\n\ndata: [DONE]\n\n", raw)
	return b.String()
}

// barrierTool answers only once every call in the batch arrives, so a sequential agent deadlocks
type barrierTool struct {
	name    string
	arrived chan struct{}
	release chan struct{}
}

func newBarrier(names ...string) []*barrierTool {
	arrived, release := make(chan struct{}, len(names)), make(chan struct{})
	tools := make([]*barrierTool, len(names))
	for i, name := range names {
		tools[i] = &barrierTool{name: name, arrived: arrived, release: release}
	}
	return tools
}

func (b *barrierTool) Name() string        { return b.name }
func (b *barrierTool) Class() tools.Class  { return tools.ClassReads } // only ever batched with reads
func (b *barrierTool) Description() string { return "a read that waits for its neighbour" }
func (b *barrierTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
}
func (b *barrierTool) NeedsConfirm() bool                              { return false }
func (b *barrierTool) Targets(json.RawMessage) ([]tools.Target, error) { return nil, nil }
func (b *barrierTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	b.arrived <- struct{}{}
	<-b.release
	var in struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	return "result for " + firstNonEmpty(in.Path, in.Pattern), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func TestReadOnlyCallsInOneTurnRunTogether(t *testing.T) {
	pair := newBarrier("read_file", "lookup")
	stub := &stubServer{scripts: []string{
		twoCalls("read_file", `{"path":"a.go"}`, "lookup", `{"pattern":"needle"}`),
		sse("", "both read", "", nil, "stop"),
	}}
	server := httptest.NewServer(stub)
	defer server.Close()

	registry := tools.NewRegistry(pair[0], pair[1])
	a := New(llm.NewClient(server.URL+"/v1", ""), registry, "test-model", Options{MaxTurns: 3, MaxTokens: 128, CtxSize: 8000}, &captureSink{})

	done := make(chan struct{})
	go func() {
		if err := a.Ask(context.Background(), "read both"); err != nil {
			t.Errorf("ask: %v", err)
		}
		close(done)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-pair[0].arrived:
		case <-time.After(2 * time.Second):
			close(pair[0].release)
			t.Fatal("the second read never started: the calls ran one after the other")
		}
	}
	close(pair[0].release)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the run never finished")
	}

	replies := toolReplies(t, stub)
	if len(replies) != 2 {
		t.Fatalf("want two tool replies, got %v", replies)
	}
	if !strings.Contains(replies[0], "a.go") || !strings.Contains(replies[1], "needle") {
		t.Errorf("the replies are out of call order: %v", replies)
	}
}

func TestAWriteInTheBatchKeepsTheOrder(t *testing.T) {
	var order []string
	rec := &orderingTool{order: &order}
	stub := &stubServer{scripts: []string{
		twoCalls("read_file", `{"path":"a.go"}`, "write_file", `{"path":"b.go","content":"x"}`),
		sse("", "done", "", nil, "stop"),
	}}
	server := httptest.NewServer(stub)
	defer server.Close()

	registry := tools.NewRegistry(rec, &writeStub{order: &order})
	a := New(llm.NewClient(server.URL+"/v1", ""), registry, "test-model", Options{MaxTurns: 3, MaxTokens: 128, CtxSize: 8000}, &captureSink{decision: Allow})

	if err := a.Ask(context.Background(), "read then write"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "read_file,write_file" {
		t.Errorf("a batch with a change ran as %v, want the order the model asked for", order)
	}
}

type orderingTool struct{ order *[]string }

func (o *orderingTool) Name() string        { return "read_file" }
func (o *orderingTool) Class() tools.Class  { return tools.ClassReads }
func (o *orderingTool) Description() string { return "records that it ran" }
func (o *orderingTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
}
func (o *orderingTool) NeedsConfirm() bool                              { return false }
func (o *orderingTool) Targets(json.RawMessage) ([]tools.Target, error) { return nil, nil }
func (o *orderingTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	appendRun(o.order, "read_file")
	return "read", nil
}

type writeStub struct{ order *[]string }

func (w *writeStub) Name() string        { return "write_file" }
func (w *writeStub) Class() tools.Class  { return tools.ClassWrites }
func (w *writeStub) Description() string { return "records that it ran" }
func (w *writeStub) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}}}`)
}
func (w *writeStub) NeedsConfirm() bool                              { return true }
func (w *writeStub) Targets(json.RawMessage) ([]tools.Target, error) { return nil, nil }
func (w *writeStub) Run(_ context.Context, args json.RawMessage) (string, error) {
	appendRun(w.order, "write_file")
	return "wrote", nil
}

var orderMu sync.Mutex

func appendRun(order *[]string, name string) {
	orderMu.Lock()
	defer orderMu.Unlock()
	if order != nil {
		*order = append(*order, name)
	}
}

// toolReplies reads the role:"tool" messages of the last request, in the order the API wants them.
func toolReplies(t *testing.T, stub *stubServer) []string {
	t.Helper()
	requests := stub.requests
	if len(requests) < 2 {
		t.Fatalf("the model was asked %d times, want a second round", len(requests))
	}
	var out []string
	for _, raw := range requests[len(requests)-1]["messages"].([]any) {
		msg := raw.(map[string]any)
		if msg["role"] == "tool" {
			out = append(out, msg["content"].(string))
		}
	}
	return out
}
