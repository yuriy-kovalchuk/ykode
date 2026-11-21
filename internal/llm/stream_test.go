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

	turn, err := NewClient(srv.URL+"/v1").ChatStream(context.Background(), ChatRequest{Model: "m"}, nil)
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

	turn, err := NewClient(srv.URL+"/v1").ChatStream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: "user", Content: "?"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Message.Content != "42" {
		t.Fatalf("content = %q, want the turn even with nobody watching the deltas", turn.Message.Content)
	}
}

// a stream accepted without [DONE] or a finish reason says so, because a dropped connection looks the same
func TestAStreamWithNoEndIsMarkedUnfinished(t *testing.T) {
	c, _ := streamServer(t, frame(`{"choices":[{"delta":{"content":"half"}}]}`))
	turn, _, err := collect(t, c)
	if err != nil {
		t.Fatal(err)
	}
	if !turn.Unfinished {
		t.Error("a reply with no [DONE] and no finish reason was taken as complete")
	}

	c, _ = streamServer(t, frame(`{"choices":[{"delta":{"content":"whole"},"finish_reason":"stop"}]}`))
	if turn, _, err = collect(t, c); err != nil || turn.Unfinished {
		t.Errorf("a reply with a finish reason was marked unfinished (err %v)", err)
	}
}

// a tool call reaches the screen as it is written, not only once the reply is done
func TestToolCallFragmentsAreStreamed(t *testing.T) {
	c, _ := streamServer(t, frame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"write_file","arguments":"{\"path\":"}}]}}]}`)+
		frame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\"}"}}]},"finish_reason":"tool_calls"}]}`))
	_, deltas, err := collect(t, c)
	if err != nil {
		t.Fatal(err)
	}
	var args string
	for _, d := range deltas {
		if d.Call == nil {
			continue
		}
		if d.Call.Name != "write_file" || d.Call.Index != 0 {
			t.Errorf("fragment %+v lost its call's name or index", *d.Call)
		}
		args += d.Call.Args
	}
	if args != `{"path":"a.go"}` {
		t.Errorf("streamed arguments = %q, want the whole call", args)
	}
}

// a server that sends timings with every chunk has them reported as the reply streams
func TestTimingsAreReportedWhileStreaming(t *testing.T) {
	c, _ := streamServer(t, frame(`{"choices":[{"delta":{"content":"a"}}],"timings":{"predicted_n":1,"predicted_per_second":0}}`)+
		frame(`{"choices":[{"delta":{"content":"b"},"finish_reason":"stop"}],"timings":{"predicted_n":2,"predicted_per_second":38.5,"prompt_per_second":410}}`))
	_, deltas, err := collect(t, c)
	if err != nil {
		t.Fatal(err)
	}
	var speeds []float64
	for _, d := range deltas {
		if d.Timings != nil {
			speeds = append(speeds, d.Timings.PredictedPerSecond)
		}
	}
	if len(speeds) != 2 || speeds[1] != 38.5 {
		t.Errorf("speeds while streaming = %v, want one per chunk ending at 38.5", speeds)
	}
}

// a server with no timings still says what the request cost, in the stream's last chunk
func TestAServerThatOnlyCountsUsageStillReportsTheCache(t *testing.T) {
	acc := newStreamAccumulator()
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1200,\"completion_tokens\":7,\"prompt_tokens_details\":{\"cached_tokens\":1000}}}\n\n" +
		"data: [DONE]\n\n"
	if err := readSSE(strings.NewReader(stream), acc, nil); err != nil {
		t.Fatal(err)
	}
	got := acc.turn().Timings
	if got.PromptTokens() != 1200 || got.CacheN != 1000 || got.PredictedN != 7 {
		t.Errorf("read %+v, want a prompt of 1200 with 1000 cached and 7 written", got)
	}
}

// counts may come in one chunk and speeds in another, either order: assigning a block whole lost the counts
func TestCountsSurviveATimingsChunkThatCarriesOnlySpeeds(t *testing.T) {
	acc := newStreamAccumulator()
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":5000,\"completion_tokens\":20,\"prompt_tokens_details\":{\"cached_tokens\":4000}}}\n\n" +
		"data: {\"choices\":[],\"timings\":{\"predicted_ms\":100,\"predicted_per_second\":30}}\n\n" +
		"data: [DONE]\n\n"
	if err := readSSE(strings.NewReader(stream), acc, nil); err != nil {
		t.Fatal(err)
	}
	got := acc.turn().Timings
	if got.PromptTokens() != 5000 || got.CacheN != 4000 || got.PredictedN != 20 {
		t.Errorf("read %+v, want the usage counts kept beside the timings speeds", got)
	}
	if got.PredictedPerSecond != 30 {
		t.Errorf("the speed the timings chunk brought was lost: %+v", got)
	}
}

// the other order, with a cached share but no total: what was never said stays unknown
func TestCountsArrivingInEitherOrder(t *testing.T) {
	acc := newStreamAccumulator()
	stream := "data: {\"choices\":[],\"timings\":{\"cache_n\":120,\"prompt_n\":900,\"predicted_n\":5,\"prompt_per_second\":400}}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":99999,\"completion_tokens\":7}}\n\n" +
		"data: [DONE]\n\n"
	if err := readSSE(strings.NewReader(stream), acc, nil); err != nil {
		t.Fatal(err)
	}
	if got := acc.turn().Timings; got.PromptTokens() != 1020 || got.PredictedN != 5 {
		t.Errorf("read %+v, want the first block's counts to stand and its predicted count kept", got)
	}

	partial := newStreamAccumulator()
	if err := readSSE(strings.NewReader("data: {\"usage\":{\"completion_tokens\":9,\"prompt_tokens_details\":{\"cached_tokens\":4000}}}\n\ndata: [DONE]\n\n"), partial, nil); err != nil {
		t.Fatal(err)
	}
	if got := partial.turn().Timings; got.PromptTokens() != 0 || got.PredictedN != 9 {
		t.Errorf("read %+v, want a cached share without a total to leave the prompt unknown", got)
	}
}

// a stream error written as a bare string was a decode failure before, hiding the message the user needed
func TestAStreamErrorWrittenAsAStringStillReachesTheUser(t *testing.T) {
	acc := newStreamAccumulator()
	err := readSSE(strings.NewReader("data: {\"error\":\"the model is loading\"}\n\n"), acc, nil)
	if err == nil {
		t.Fatal("a stream error was accepted as an empty reply")
	}
	if !strings.Contains(err.Error(), "the model is loading") {
		t.Errorf("%v: want the server's own words", err)
	}
	if !modelIsLoading(err.Error()) {
		t.Errorf("%v: want a loading server to be recognised as one worth asking again", err)
	}
}

// a server reading a long prompt says how far it is, and each report reaches the caller as it comes
func TestChatStreamPassesOnHowFarThePromptHasBeenRead(t *testing.T) {
	sse := frame(`{"choices":[],"prompt_progress":{"total":9000,"cache":1000,"processed":4000,"time_ms":800}}`) +
		frame(`{"choices":[],"prompt_progress":{"total":9000,"cache":1000,"processed":9000,"time_ms":2100}}`) +
		frame(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`) +
		"data: [DONE]\n\n"
	c, _ := streamServer(t, sse)
	turn, deltas, err := collect(t, c)
	if err != nil {
		t.Fatal(err)
	}
	var read []int
	for _, d := range deltas {
		if d.Progress != nil {
			read = append(read, d.Progress.Processed)
		}
	}
	if fmt.Sprint(read) != "[4000 9000]" || turn.Message.Content != "ok" {
		t.Errorf("read %v, answer %q: want both reports in order and the answer untouched", read, turn.Message.Content)
	}
}
