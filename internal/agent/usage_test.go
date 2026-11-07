package agent

import (
	"context"
	"fmt"
	"strings"
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
	a, cleanup := newAgent(t, stub, Options{}, nullSink{})
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
	a, cleanup := newAgent(t, stub, Options{}, nullSink{})
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
	a, cleanup := newAgent(t, stub, Options{}, nullSink{})
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
	a, cleanup := newAgent(t, stub, Options{}, nullSink{})
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

func TestUsageLineShowsWhatTheConversationCost(t *testing.T) {
	line := UsageLine(Usage{
		Turns: 3, PromptTok: 1800, CachedTok: 1600, GenTok: 240, TotalPrompt: 5200,
		EstTok: 2000, BytesPerTok: 3.6,
	}, 131072)
	for _, want := range []string{"3 turns", "prompt 1800", "cached 1600", "generated 240", "~2000 of 131072", "3.6 bytes/token"} {
		if !strings.Contains(line, want) {
			t.Errorf("missing %q in: %s", want, line)
		}
	}
}

func TestUsageLineBeforeTheFirstRequest(t *testing.T) {
	if line := UsageLine(Usage{}, 131072); !strings.Contains(line, "nothing asked yet") {
		t.Errorf("line = %q, want it to say there is nothing to report", line)
	}
}

func TestUsageLineWithoutAKnownContextWindow(t *testing.T) {
	line := UsageLine(Usage{Turns: 1, PromptTok: 100, EstTok: 120, BytesPerTok: 3.3}, 0)
	if strings.Contains(line, "of 0") {
		t.Errorf("line = %q, want no context window mentioned when the server never reported one", line)
	}
}

func TestADraftModelsWorkIsCounted(t *testing.T) {
	m := newMeter()
	m.observe(Stats{PromptTokens: 100, DraftN: 200, DraftNAccepted: 120}, 300)
	m.observe(Stats{PromptTokens: 100, DraftN: 100, DraftNAccepted: 40}, 300)
	// like the cache and tok/s figures, acceptance describes the turn that just ran
	if got := acceptance(m.draftOK, m.draft); got != 0.4 {
		t.Errorf("acceptance should follow the newest turn: %v", got)
	}
	m.clearCost()
	if m.draft != 0 || m.draftOK != 0 {
		t.Error("clearCost left the draft counters behind")
	}
}

func TestTheContextLineMentionsSpeculationOnlyWhenThereWasAny(t *testing.T) {
	a := New(nil, nil, "m", Options{CtxSize: 8000}, nil)
	a.report = Usage{Turns: 1, PromptTok: 500, CachedTok: 400}
	if line := a.ContextLine(); strings.Contains(line, "speculative") {
		t.Errorf("a server with no draft model: %s", line)
	}
	a.report = Usage{Turns: 2, PromptTok: 500, CachedTok: 400, Draft: 100, DraftOK: 60, Acceptance: 0.6}
	line := a.ContextLine()
	if !strings.Contains(line, "speculative decoding: 60%") || !strings.Contains(line, "60 of 100") {
		t.Errorf("acceptance missing: %s", line)
	}
	if !strings.Contains(line, "cached (80%)") {
		t.Errorf("the cache hit rate is missing: %s", line)
	}
}

func TestTheDraftCountersReachTheUsageSnapshot(t *testing.T) {
	script := `data: {"choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}` +
		"\n\ndata: {\"choices\":[],\"timings\":{\"prompt_n\":100,\"predicted_n\":20,\"draft_n\":50,\"draft_n_accepted\":30}}\n\n"
	a, cleanup := newAgent(t, &stubServer{scripts: []string{script}}, Options{}, nullSink{})
	defer cleanup()

	if err := a.Ask(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	u := a.Usage()
	if u.Draft != 50 || u.DraftOK != 30 || u.Acceptance != 0.6 {
		t.Errorf("usage lost the draft counters: %+v", u)
	}
	if !strings.Contains(a.ContextLine(), "speculative decoding: 60%") {
		t.Errorf("and /ctx does not show it: %s", a.ContextLine())
	}
}
