package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func frame(payload string) string { return "data: " + payload + "\n\n" }

// streamServer answers a chat request with raw SSE text and records the body it received.

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
	return NewClient(srv.URL+"/v1", ""), &got
}

func collect(t *testing.T, c *Client) (Turn, []Delta, error) {
	t.Helper()
	var deltas []Delta
	turn, err := c.ChatStream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, func(d Delta) {
		deltas = append(deltas, d)
	})
	return turn, deltas, err
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
		frame(`{"choices":[],"timings":{"cache_n":120,"prompt_n":961,"predicted_n":60,"prompt_per_second":456.2,"predicted_per_second":34.2,"draft_n":80,"draft_n_accepted":52}}`)
	c, _ := streamServer(t, sse)

	turn, _, err := collect(t, c)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Timings.PromptTokens() != 1081 || turn.Timings.PredictedPerSecond != 34.2 {
		t.Fatalf("timings = %+v", turn.Timings)
	}
	// a router running a draft model reports what it proposed and what was kept
	if turn.Timings.DraftN != 80 || turn.Timings.DraftNAccepted != 52 {
		t.Errorf("the draft counters were dropped: %+v", turn.Timings)
	}
}

func TestChatStreamQuotesTheServerErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"Failed to parse tool call arguments as JSON"}`)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL+"/v1", "").ChatStream(context.Background(), ChatRequest{}, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") || !strings.Contains(err.Error(), "tool call arguments") {
		t.Fatalf("error = %v, want the status and the quoted body", err)
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

	models, err := NewClient(srv.URL+"/v1", "").Models(context.Background())
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
	if _, err := NewClient(empty.URL+"/v1", "").Models(context.Background()); err == nil {
		t.Fatal("an empty model list should be an error")
	}

	unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "no healthy upstream")
	}))
	defer unavailable.Close()
	_, err := NewClient(unavailable.URL+"/v1", "").Models(context.Background())
	if err == nil || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "no healthy upstream") {
		t.Fatalf("error = %v", err)
	}
}

func flakyServer(t *testing.T, fails int, status int, body string) (*Client, *atomic.Int32) {
	t.Helper()
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= int32(fails) {
			w.WriteHeader(status)
			fmt.Fprint(w, body)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, frame(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL+"/v1", ""), &attempts
}

func TestChatStreamRetriesATransientGatewayFailure(t *testing.T) {
	c, attempts := flakyServer(t, 1, http.StatusBadGateway, "upstream went away")
	turn, _, err := collect(t, c)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Message.Content != "ok" {
		t.Errorf("answer = %q, want the retry to succeed", turn.Message.Content)
	}
	if attempts.Load() != 2 {
		t.Errorf("attempts = %d, want 2", attempts.Load())
	}
}

func TestChatStreamDoesNotReplayADeterministicServerError(t *testing.T) {
	c, attempts := flakyServer(t, 5, http.StatusInternalServerError, `{"error":"Failed to parse tool call arguments as JSON"}`)
	_, _, err := collect(t, c)
	if err == nil {
		t.Fatal("want an error")
	}
	if attempts.Load() != 1 {
		t.Errorf("attempts = %d, want 1: replaying a bad request only wastes a turn", attempts.Load())
	}
}

func TestChatStreamNamesALoadingModel(t *testing.T) {
	c, attempts := flakyServer(t, 5, http.StatusServiceUnavailable, `{"error":"model is loading"}`)
	_, _, err := collect(t, c)
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "may still be starting") {
		t.Errorf("error = %v, want the hint that the server is still loading the model", err)
	}
	if attempts.Load() != chatAttempts {
		t.Errorf("attempts = %d, want %d", attempts.Load(), chatAttempts)
	}
}

func TestChatStreamRetriesADroppedConnection(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL+"/v1", "").ChatStream(context.Background(),
		ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
	if err == nil {
		t.Fatal("want an error")
	}
	if attempts.Load() != chatAttempts {
		t.Errorf("attempts = %d, want %d", attempts.Load(), chatAttempts)
	}
}

func TestChatStreamStopsPromptlyWhenCancelled(t *testing.T) {
	c, attempts := flakyServer(t, 5, http.StatusServiceUnavailable, "model is loading")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := c.ChatStream(ctx, ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if attempts.Load() != 0 {
		t.Errorf("attempts = %d, want no request at all", attempts.Load())
	}
	if spent := time.Since(start); spent > retryWait/2 {
		t.Errorf("waited %v after the user cancelled, want no retry delay at all", spent)
	}
}

func TestTheBearerTokenGoesWithEveryRequest(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"m","meta":{"n_ctx":8192}}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/v1", "secret-token")
	if _, err := c.Models(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ChatStream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil); err != nil {
		t.Fatal(err)
	}
	for _, got := range seen {
		if got != "Bearer secret-token" {
			t.Errorf("Authorization = %q, want the bearer token on /models and /chat", got)
		}
	}
}

func TestNoAuthHeaderWithoutAKey(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"data":[{"id":"m","meta":{"n_ctx":8192}}]}`)
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL+"/v1", "").Models(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("Authorization = %q, want no header when no key is configured", got)
	}
}

// a stream that started answering and then stops is dead; one that never started is waited for
func TestChatStreamGivesUpOnAStreamThatGoesQuiet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"x"}}]}`+"\n\n")
		w.(http.Flusher).Flush() // one chunk arrives, then silence
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/v1", "")
	c.Idle = 50 * time.Millisecond
	_, err := c.ChatStream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "sent nothing for 50ms") {
		t.Fatalf("ChatStream() error = %v, want the gap between tokens named", err)
	}
}

// a server reading a prompt rewritten by a fold sends nothing for a while: that is not a dead
// stream, so the wait for the first byte is not the wait between tokens
func TestChatStreamWaitsForAServerToStartAnswering(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		time.Sleep(120 * time.Millisecond) // longer than a token gap, shorter than the opening wait
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"late"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
		w.(http.Flusher).Flush()
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/v1", "")
	c.Idle, c.opening = 40*time.Millisecond, 2*time.Second
	turn, err := c.ChatStream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
	if err != nil {
		t.Fatalf("a slow start was cut off: %v", err)
	}
	if turn.Message.Content != "late" {
		t.Fatalf("content = %q", turn.Message.Content)
	}
}

// the opening wait still ends a stream that never began, and the error names that wait
func TestChatStreamGivesUpWhenNoByteArrives(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush() // headers arrive, then nothing at all
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/v1", "")
	c.Idle, c.opening = time.Minute, 50*time.Millisecond
	_, err := c.ChatStream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "sent nothing for 50ms") {
		t.Fatalf("ChatStream() error = %v, want the opening wait named", err)
	}
}

func TestChatStreamTakesAsLongAsItNeedsBetweenChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 4; i++ {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(40 * time.Millisecond)
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/v1", "")
	c.Idle = 150 * time.Millisecond // longer than any gap, shorter than the whole stream
	turn, err := c.ChatStream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
	if err != nil {
		t.Fatalf("a slow but living stream must not be cut off: %v", err)
	}
	if turn.Message.Content != "xxxx" {
		t.Fatalf("content = %q", turn.Message.Content)
	}
}

func TestTheWireNeverCarriesYkodeOwnResultIds(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL+"/v1", "").ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "tool", Name: "read_file", ToolCallID: "c1", CallID: "r1", Content: "the text"}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatal(err)
	}
	if len(sent.Messages) != 1 {
		t.Fatalf("the request carried %d messages", len(sent.Messages))
	}
	if _, ok := sent.Messages[0]["call_id"]; ok {
		t.Errorf("ykode's own result id went to the server: %s", body)
	}
	if _, ok := sent.Messages[0]["tool_call_id"]; !ok {
		t.Errorf("the server's own id went missing from the request: %s", body)
	}
}

// a conversation is one conversation to the server: every request names the run, so a proxy in
// front of the server can keep it on the slot that already holds its prompt cache
func TestEveryRequestNamesTheRunItBelongsTo(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("X-Session-Id"))
		switch r.URL.Path {
		case "/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"m","n_ctx":4096}]}`)
		default:
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, frame(`{"choices":[{"delta":{"content":"hi"}}]}`)+"data: [DONE]\n\n")
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/v1", "")
	if _, err := c.Models(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ChatStream(context.Background(), ChatRequest{Model: "m"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, got := range seen {
		if got != "" {
			t.Errorf("a request carried %q before the run was named", got)
		}
	}

	c.Session = "20261108-1930.000"
	seen = nil
	if _, err := c.Models(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ChatStream(context.Background(), ChatRequest{Model: "m"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != c.Session || seen[1] != c.Session {
		t.Errorf("requests carried %v, want the run on both", seen)
	}
}

// The second failure of a retried turn is often the blander of the two; the first one usually names
// the reason, so it travels with the error the user reads.
func TestARetriedFailureKeepsTheFirstReason(t *testing.T) {
	c, attempts := flakyServer(t, 5, http.StatusServiceUnavailable, "model is loading")
	_, _, err := collect(t, c)
	if err == nil {
		t.Fatal("want an error")
	}
	if attempts.Load() != chatAttempts {
		t.Errorf("attempts = %d, want %d", attempts.Load(), chatAttempts)
	}
	if !strings.Contains(err.Error(), "model is loading") {
		t.Errorf("%v: want the first failure's reason kept beside the second", err)
	}
}
