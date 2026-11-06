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

// frame builds one SSE data line, as the server would send it.
func frame(payload string) string { return "data: " + payload + "\n\n" }

// streamServer answers a chat request with raw SSE text and records the request
// body it received.
func streamServer(t *testing.T, sse string) (*Client, *ChatRequest) {
	t.Helper()
	var got ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse)
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL + "/v1"), &got
}

func collect(t *testing.T, c *Client) (Turn, []Delta, error) {
	t.Helper()
	var deltas []Delta
	turn, err := c.ChatStream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, func(d Delta) {
		deltas = append(deltas, d)
	})
	return turn, deltas, err
}

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
	// One data line bigger than maxLineBytes must surface as an error, not a
	// silently truncated message.
	c, _ := streamServer(t, frame(`{"choices":[{"delta":{"content":"`+strings.Repeat("x", maxLineBytes+16)+`"}}]}`))
	if _, _, err := collect(t, c); err == nil || !strings.Contains(err.Error(), "read stream") {
		t.Fatalf("error = %v", err)
	}
}

func TestChatStreamQuotesTheServerErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"Failed to parse tool call arguments as JSON"}`)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL+"/v1").ChatStream(context.Background(), ChatRequest{}, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") || !strings.Contains(err.Error(), "tool call arguments") {
		t.Fatalf("error = %v, want the status and the quoted body", err)
	}
}

func TestChatStreamRequestIsAlwaysStreamed(t *testing.T) {
	body := ChatRequest{
		Model:              "m",
		Messages:           []Message{{Role: "user", Content: "go"}},
		Tools:              []ToolSpec{{Type: "function", Function: ToolFunction{Name: "list_dir", Parameters: json.RawMessage(`{"type":"object"}`)}}},
		ToolChoice:         "auto",
		MaxTokens:          512,
		ChatTemplateKwargs: map[string]any{"enable_thinking": false},
	}
	sse := frame(`{"choices":[{"delta":{"content":"ok"}}],"finish_reason":"stop"}`)
	c, got := streamServer(t, sse)
	var rendered int
	if _, err := c.ChatStream(context.Background(), body, func(Delta) { rendered++ }); err != nil {
		t.Fatal(err)
	}
	if rendered == 0 {
		t.Fatal("no deltas reached the renderer")
	}
	if !got.Stream {
		t.Fatal("stream must always be true")
	}
	if got.MaxTokens != 512 || got.ToolChoice != "auto" || got.ChatTemplateKwargs["enable_thinking"] != false {
		t.Fatalf("request lost fields: %+v", got)
	}
	if got.Messages[0].Reasoning != "" {
		t.Fatal("thinking must not be sent back")
	}
}

func TestChatStreamKeepsServerTimings(t *testing.T) {
	sse := frame(`{"choices":[{"delta":{"content":"x"}}]}`) +
		frame(`{"choices":[],"timings":{"cache_n":120,"prompt_n":961,"predicted_n":60,"prompt_per_second":456.2,"predicted_per_second":34.2}}`)
	c, _ := streamServer(t, sse)

	turn, _, err := collect(t, c)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Timings.PromptTokens() != 1081 || turn.Timings.PredictedPerSecond != 34.2 {
		t.Fatalf("timings = %+v", turn.Timings)
	}
}

func TestChatStreamRefusesAPlainJSONAnswer(t *testing.T) {
	// A server that answers a streaming request with a single JSON body (an
	// error proxy, a load balancer) must not look like an empty model reply.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error":{"message":"model is not loaded","code":400}}`)
	}))
	defer srv.Close()

	turn, err := NewClient(srv.URL+"/v1").ChatStream(context.Background(), ChatRequest{Model: "m"}, nil)
	if err == nil {
		t.Fatalf("no error, got turn %+v", turn)
	}
	if !strings.Contains(err.Error(), "model is not loaded") || !strings.Contains(err.Error(), "event stream") {
		t.Fatalf("error = %v, want it to name the body and the expected stream", err)
	}
}

func TestModelsReadsContextSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":[
		  {"id":"loaded","meta":{"n_ctx":8192}},
		  {"id":"unloaded","status":{"args":["llama-server","--ctx-size","131072","-m","x.gguf"]}},
		  {"id":"silent"},
		  {"id":""}]}`)
	}))
	defer srv.Close()

	models, err := NewClient(srv.URL + "/v1").Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []ModelInfo{{ID: "loaded", CtxSize: 8192}, {ID: "unloaded", CtxSize: 131072}, {ID: "silent"}}
	if len(models) != len(want) {
		t.Fatalf("models = %+v", models)
	}
	for i := range want {
		if models[i] != want[i] {
			t.Errorf("model %d = %+v, want %+v", i, models[i], want[i])
		}
	}
}

func TestModelsReportsEmptyAndFailedServers(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer empty.Close()
	if _, err := NewClient(empty.URL + "/v1").Models(context.Background()); err == nil {
		t.Fatal("an empty model list should be an error")
	}

	unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "no healthy upstream")
	}))
	defer unavailable.Close()
	_, err := NewClient(unavailable.URL + "/v1").Models(context.Background())
	if err == nil || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "no healthy upstream") {
		t.Fatalf("error = %v", err)
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
