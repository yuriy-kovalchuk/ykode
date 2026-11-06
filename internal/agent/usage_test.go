package agent

import (
	"context"
	"fmt"
	"testing"
)

// answerWithTimings is one streamed answer plus the accounting llama.cpp sends with the last chunk.
func answerWithTimings(answer string, promptN, cacheN, predictedN int) string {
	return fmt.Sprintf(`data: {"choices":[{"index":0,"delta":{"content":%q},"finish_reason":"stop"}]}`, answer) +
		fmt.Sprintf("\n\ndata: {\"choices\":[],\"timings\":{\"prompt_n\":%d,\"cache_n\":%d,\"predicted_n\":%d}}\n\n",
			promptN, cacheN, predictedN)
}

func TestUsageAddsUpTheServersOwnNumbers(t *testing.T) {
	stub := &stubServer{scripts: []string{answerWithTimings("done", 900, 100, 40)}}
	a, cleanup := newAgent(t, stub, Options{}, quietSink{})
	defer cleanup()

	if err := a.Ask(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	u := a.Usage()
	if u.Turns != 1 || u.PromptTok != 1000 || u.CachedTok != 100 || u.GenTok != 40 {
		t.Errorf("usage = %+v, want the numbers the server reported", u)
	}
}

func TestTheEstimateTracksTheServersCount(t *testing.T) {
	stub := &stubServer{scripts: []string{answerWithTimings("ok", 1400, 0, 5)}}
	a, cleanup := newAgent(t, stub, Options{}, quietSink{})
	defer cleanup()

	if err := a.Ask(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	u := a.Usage()
	if u.PromptTok == 0 {
		t.Fatal("the server reported no prompt tokens")
	}
	if drift := u.EstTok - u.PromptTok; drift > u.PromptTok/4 || drift < -u.PromptTok/4 {
		t.Errorf("estimate %d is more than 25%% off the server's own count of %d", u.EstTok, u.PromptTok)
	}
}

func TestAnImplausibleServerCountLeavesTheEstimateAlone(t *testing.T) {
	stub := &stubServer{scripts: []string{answerWithTimings("ok", 5, 0, 5)}}
	a, cleanup := newAgent(t, stub, Options{}, quietSink{})
	defer cleanup()

	if err := a.Ask(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if got := a.Usage().BytesPerTok; got != defaultBytesPerTok {
		t.Errorf("bytes per token = %v, want the default kept when the server's count makes no sense", got)
	}
}

func TestResetClearsTheCostButKeepsTheLesson(t *testing.T) {
	stub := &stubServer{scripts: []string{answerWithTimings("ok", 1400, 0, 5)}}
	a, cleanup := newAgent(t, stub, Options{}, quietSink{})
	defer cleanup()

	if err := a.Ask(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	learned := a.Usage().BytesPerTok
	if learned == defaultBytesPerTok {
		t.Fatal("the calibration did not move")
	}
	a.Reset()
	after := a.Usage()
	if after.Turns != 0 || after.GenTok != 0 {
		t.Errorf("usage = %+v, want the counters cleared", after)
	}
	if after.BytesPerTok != learned {
		t.Errorf("bytes per token = %v, want the learned ratio kept across /reset", after.BytesPerTok)
	}
}

func TestTheEstimateIncludesTheSystemPrompt(t *testing.T) {
	with := newForHistory(Options{})
	without := newForHistory(Options{})
	without.history = nil
	if with.sendingBytes() <= without.sendingBytes() {
		t.Fatal("the system prompt must count toward the bytes we send")
	}
}
