package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatStreamSeparatesThinkingFromAnswer(t *testing.T) {
	sse := frame(`{"choices":[{"delta":{"role":"assistant"}}]}`) +
		frame(`{"choices":[{"delta":{"reasoning_content":"think "}}]}`) +
		frame(`{"choices":[{"delta":{"reasoning_content":"more"}}]}`) +
		frame(`{"choices":[{"delta":{"content":"the "}}]}`) +
		frame(`{"choices":[{"delta":{"content":"answer"},"finish_reason":"stop"}]}`) +
		"data: [DONE]\n\n"
	c, _ := streamServer(t, sse)

	turn, deltas, err := collect(t, c)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Message.Content != "the answer" || turn.Message.Reasoning != "think more" {
		t.Fatalf("turn = %+v, want the answer and the thinking kept apart", turn.Message)
	}
	if turn.FinishReason != "stop" {
		t.Fatalf("finish = %q", turn.FinishReason)
	}
	if len(deltas) != 4 || deltas[0].Reasoning == "" || deltas[2].Content == "" {
		t.Fatalf("deltas = %+v, want four in arrival order", deltas)
	}
}

func TestChatStreamStitchesToolCallFragments(t *testing.T) {
	sse := frame(`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"b","type":"function","function":{"name":"read_file","arguments":"{\"path\":\""}}]}}]}`) +
		frame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"list_dir","arguments":"{\""}}]}}]}`) +
		frame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"path\":\".\"}"}}]}}]}`) +
		frame(`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"x\"}"}}]}}]}`) +
		frame(`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)
	c, _ := streamServer(t, sse)

	turn, _, err := collect(t, c)
	if err != nil {
		t.Fatal(err)
	}
	calls := turn.Message.ToolCalls
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	// Fragments interleave across indexes; the call that started first comes back first.
	if calls[0].ID != "b" || calls[0].Function.Name != "read_file" {
		t.Fatalf("call 0 = %+v, want the call that started first", calls[0])
	}
	if args, err := calls[0].Function.ArgsJSON(); err != nil || string(args) != `{"path":"x"}` {
		t.Fatalf("call 0 args = %s (%v), want the fragments rejoined", args, err)
	}
	if calls[1].ID != "a" || calls[1].Function.Name != "list_dir" {
		t.Fatalf("call 1 = %+v", calls[1])
	}
	if args, err := calls[1].Function.ArgsJSON(); err != nil || string(args) != `{"path":"."}` {
		t.Fatalf("call 1 args = %s (%v)", args, err)
	}
	if turn.FinishReason != "tool_calls" {
		t.Fatalf("finish = %q", turn.FinishReason)
	}
}

func TestChatStreamAcceptsStreamEndingWithoutDone(t *testing.T) {
	c, _ := streamServer(t, frame(`{"choices":[{"delta":{"content":"half"}}]}`))
	turn, _, err := collect(t, c)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Message.Content != "half" {
		t.Fatalf("content = %q", turn.Message.Content)
	}
}

func TestChatStreamReportsBrokenChunksAndServerErrors(t *testing.T) {
	cases := []struct {
		name, sse, want string
	}{
		{"malformed chunk", "data: {\"choices\":[\n\n", "decode stream chunk"},
		{"error chunk", frame(`{"error":{"message":"context length exceeded","code":400}}`), "context length exceeded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := streamServer(t, tc.sse)
			if _, _, err := collect(t, c); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestChatStreamRefusesAnOverlongLine(t *testing.T) {
	// a data line over maxLineBytes must surface as an error, not a silently truncated message
	c, _ := streamServer(t, frame(`{"choices":[{"delta":{"content":"`+strings.Repeat("x", maxLineBytes+16)+`"}}]}`))
	if _, _, err := collect(t, c); err == nil || !strings.Contains(err.Error(), "read stream") {
		t.Fatalf("error = %v", err)
	}
}

func TestChatStreamRefusesAPlainJSONAnswer(t *testing.T) {
	// an error proxy answering a streaming request with one JSON body is not an empty reply
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error":{"message":"model is not loaded","code":400}}`)
	}))
	defer srv.Close()

	turn, err := NewClient(srv.URL+"/v1", "").ChatStream(context.Background(), ChatRequest{Model: "m"}, nil)
	if err == nil {
		t.Fatalf("no error, got turn %+v", turn)
	}
	if !strings.Contains(err.Error(), "model is not loaded") || !strings.Contains(err.Error(), "event stream") {
		t.Fatalf("error = %v, want it to name the body and the expected stream", err)
	}
}

func TestArgsJSONAcceptsBothWireShapes(t *testing.T) {
	cases := []struct {
		raw, want string
		err       bool
	}{
		{`{"path":"."}`, `{"path":"."}`, false},
		{`"{\"path\":\".\"}"`, `{"path":"."}`, false},
		{``, `{}`, false},
		{`"broken`, ``, true},
		{`{"path": "`, ``, true},
	}
	for _, tc := range cases {
		args, err := FunctionCall{Arguments: json.RawMessage(tc.raw)}.ArgsJSON()
		if tc.err {
			if err == nil {
				t.Errorf("%q: expected an error", tc.raw)
			}
			continue
		}
		if err != nil || string(args) != tc.want {
			t.Errorf("%q = %q (%v), want %q", tc.raw, args, err, tc.want)
		}
	}
}

// flakyServer fails the first fails requests, then streams one short answer.

func TestChatStreamWorksWithoutADeltaCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"hmm\",\"content\":\"42\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	turn, err := NewClient(srv.URL+"/v1", "").ChatStream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "?"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Message.Content != "42" {
		t.Fatalf("content = %q, want the turn even with nobody watching the deltas", turn.Message.Content)
	}
}
