package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// answerWithTimings is one streamed answer plus the accounting llama.cpp sends with the last chunk.
func answerWithTimings(answer string, promptN, cacheN, predictedN int) string {
	return fmt.Sprintf(`data: {"choices":[{"index":0,"delta":{"content":%q},"finish_reason":"stop"}]}`, answer) +
		fmt.Sprintf("\n\ndata: {\"choices\":[],\"timings\":{\"prompt_n\":%d,\"cache_n\":%d,\"predicted_n\":%d}}\n\n",
			promptN, cacheN, predictedN)
}

// answerWithUsage is one streamed answer plus the counts a server sends when it counts tokens but
// has no timings of its own; it says nothing about how fast the reply was.
func answerWithUsage(answer string, promptN, cachedN, completionN int) string {
	return fmt.Sprintf(`data: {"choices":[{"index":0,"delta":{"content":%q},"finish_reason":"stop"}]}`, answer) +
		fmt.Sprintf("\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d,"+
			"\"prompt_tokens_details\":{\"cached_tokens\":%d}}}\n\n", promptN, completionN, cachedN) +
		"data: [DONE]\n\n"
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
	stub := &stubServer{scripts: []string{answerWithTimings("ok", 600, 0, 5)}}
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
	if !strings.Contains(line, "in the last reply") {
		t.Errorf("acceptance does not say which reply it describes: %s", line)
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

// the conversation's cache total is its own number, apart from what the last request cached
func TestTheCacheTotalAddsUpOverTheWholeConversation(t *testing.T) {
	stub := &stubServer{scripts: []string{
		answerWithTimings("one", 900, 800, 20),
		answerWithTimings("two", 1100, 1000, 30),
	}}
	a, cleanup := newAgent(t, stub, Options{}, nullSink{})
	defer cleanup()
	for _, prompt := range []string{"first", "second"} {
		if err := a.Ask(context.Background(), prompt); err != nil {
			t.Fatal(err)
		}
	}
	u := a.Usage()
	if u.TotalPrompt != 3800 || u.CachedTotal != 1800 {
		t.Errorf("totals = %d prompt, %d cached; want 3800 and 1800", u.TotalPrompt, u.CachedTotal)
	}
	if u.CachedTok != 1000 {
		t.Errorf("the last request cached %d, want 1000: the total must not replace it", u.CachedTok)
	}
}

// a server that counts tokens but never a speed keeps its own count: only the speed comes from here
func TestAServersOwnCountsStandWhenItReportsNoSpeed(t *testing.T) {
	stub := &stubServer{scripts: []string{answerWithUsage("done", 900, 800, 40)}}
	a, cleanup := newAgent(t, stub, Options{}, nullSink{})
	defer cleanup()

	if err := a.Ask(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	u := a.Usage()
	if u.GenTok != 40 {
		t.Errorf("GenTok = %d, want the 40 the server counted, not a count measured here", u.GenTok)
	}
	if u.TotalPrompt != 900 || u.CachedTotal != 800 {
		t.Errorf("the prompt came over as %d in all, %d cached; want 900 and 800", u.TotalPrompt, u.CachedTotal)
	}
}

// where the server counts nothing at all, the count still comes from this side
func TestAGeneratedCountIsEstimatedWhereTheServerCountsNothing(t *testing.T) {
	long := strings.Repeat("a", 32) // the default ratio reads this as about ten tokens
	stub := &stubServer{scripts: []string{sse("", long, "", nil, "stop")}}
	a, cleanup := newAgent(t, stub, Options{}, nullSink{})
	defer cleanup()

	if err := a.Ask(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if got := a.Usage().GenTok; got <= 0 {
		t.Errorf("GenTok = %d, want an estimate where the server reports nothing", got)
	}
}

// the total is kept as the conversation grows, not walked: an estimate called several times a
// round-trip and once per message in the trim loop must not reread every message each time. The count
// of messages is what proves the incremental path is the one taken.
func TestTheByteTotalIsKeptRatherThanRewalked(t *testing.T) {
	a := newForHistory(Options{})
	a.add(llm.Message{Role: "user", Content: "one"})
	if a.histBytes != historyBytes(a.history) || a.histCount != len(a.history) {
		t.Fatalf("total %d over %d messages, want it to match the conversation", a.histBytes, a.histCount)
	}
	grown := append([]llm.Message(nil), a.history...)
	grown = append(grown, llm.Message{Role: "user", Content: "two"})
	a.history = grown // assigned, not added: the count no longer matches
	want := historyBytes(grown)
	if got := a.sendingBytes() - a.wireSpecBytes(); got != want {
		t.Errorf("after an assignment the total is %d, want %d", got, want)
	}
	if a.histCount != len(a.history) {
		t.Errorf("count %d after the check, want %d", a.histCount, len(a.history))
	}
}

// A fold and a drop take messages out of a conversation the server already counted, so the count it
// gave is still true for what is left: it shrinks rather than being thrown away. Zeroing it sent every
// later estimate back to a byte ratio over the whole conversation, which is where the guess comes from.
func TestAFoldKeepsTheCountTheServerGave(t *testing.T) {
	a := newForHistory(Options{CtxSize: 8000})
	a.meter.bytesPerTok = 4
	a.meter.anchorTokens, a.meter.anchorBytes = 5000, 20000
	a.meter.shrinkAnchor(4000) // a unit of four thousand bytes leaves the conversation
	if a.meter.anchorTokens != 4000 || a.meter.anchorBytes != 16000 {
		t.Errorf("anchor = %d tokens over %d bytes, want 4000 over 16000", a.meter.anchorTokens, a.meter.anchorBytes)
	}
	// and the estimate goes on using it: only what moved since needs guessing
	a.history = []llm.Message{{Role: "system", Content: strings.Repeat("x", 16000)}, {Role: "user", Content: strings.Repeat("y", 8000)}}
	a.recountBytes()
	got := a.estimateTokens()
	if want := 4000 + 8000/4; got != want {
		t.Errorf("estimate %d, want the count kept and only the new 8000 bytes guessed: %d", got, want)
	}
}

// A refusal that names the size it rejected is the one exact number available at the moment the
// estimate is known to be wrong, so it becomes the count instead of nudging the ratio.
func TestARefusalThatNamesASizeBecomesTheCount(t *testing.T) {
	a := newForHistory(Options{})
	a.meter.toldByServer(9000, 36000)
	if a.meter.anchorTokens != 9000 || a.meter.anchorBytes != 36000 {
		t.Errorf("anchor = %d over %d, want the numbers the server stated", a.meter.anchorTokens, a.meter.anchorBytes)
	}
	if a.meter.bytesPerTok != 4 {
		t.Errorf("ratio = %v, want the one those two numbers imply", a.meter.bytesPerTok)
	}
	a.meter.toldByServer(0, 36000) // a server that gave no number leaves the count alone
	if a.meter.anchorTokens != 9000 {
		t.Errorf("a refusal naming nothing overwrote the count: %d", a.meter.anchorTokens)
	}
}
