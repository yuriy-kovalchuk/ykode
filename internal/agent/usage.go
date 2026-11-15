package agent

import (
	"fmt"
	"strings"
)

// Usage is what the conversation has cost, in the server's own numbers.
type Usage struct {
	Turns       int
	PromptTok   int // prompt tokens of the last request
	CachedTok   int // of those, served from the server's prompt cache
	GenTok      int // tokens generated over the whole conversation
	TotalPrompt int // prompt tokens over the whole conversation
	CachedTotal int // of those, what the cache served over the whole conversation
	EstTok      int // estimate for the request that would go out next
	BytesPerTok float64
	Draft       int     // tokens a draft model proposed in the last reply, where the server runs one
	DraftOK     int     // of those, what the model kept
	Acceptance  float64 // DraftOK over Draft, of the last reply: like the cache share, it says how the round just ran
}

const (
	defaultBytesPerTok = 3.3
	minBytesPerTok     = 1.5 // a ratio this far off means the server told us something else
	maxBytesPerTok     = 8.0
)

// meter adds up the turns and learns the byte ratio the server counts with.
type meter struct {
	turns, prompt, cached, generated, totalPrompt, cachedTotal int
	draft, draftOK                                             int
	bytesPerTok                                                float64
	anchorTokens, anchorBytes                                  int // what the server counted for a conversation this size
}

// percent of part in whole, 0 when there was nothing to compare against.
func percent(part, whole int) int {
	if whole <= 0 {
		return 0
	}
	return part * 100 / whole
}

// acceptance is the share of a draft model's tokens the real model kept.
func acceptance(kept, proposed int) float64 {
	if proposed <= 0 {
		return 0
	}
	return float64(kept) / float64(proposed)
}

func newMeter() meter { return meter{bytesPerTok: defaultBytesPerTok} }

func (m *meter) clearCost() {
	m.turns, m.prompt, m.cached, m.generated, m.totalPrompt, m.cachedTotal = 0, 0, 0, 0, 0, 0
	m.draft, m.draftOK = 0, 0
	m.anchorTokens, m.anchorBytes = 0, 0 // the conversation this number was counted for is gone
}

// observe folds one turn in; a server that sends no timings only leaves the counters at zero.
func (m *meter) observe(stats Stats, sentBytes int) {
	m.turns++
	m.prompt, m.cached = stats.PromptTokens, stats.CachedTokens
	m.draft, m.draftOK = stats.DraftN, stats.DraftNAccepted
	m.generated += stats.OutputTokens
	m.totalPrompt += stats.PromptTokens
	m.cachedTotal += stats.CachedTokens
	if stats.PromptTokens <= 0 || sentBytes <= 0 {
		return
	}
	// the one token count in the run that is not a guess: what the server said the conversation it just
	// read was. Everything added after it is measured against the ratio this pair teaches.
	m.anchorTokens, m.anchorBytes = stats.PromptTokens, sentBytes
	if ratio := float64(sentBytes) / float64(stats.PromptTokens); ratio >= minBytesPerTok && ratio <= maxBytesPerTok {
		m.bytesPerTok = ratio
	}
}

// toldByServer records a count a server named in a refusal, which is the only exact measurement there
// is at the moment the estimate is known to have been wrong. It replaces the count, and the byte side
// comes from what this run built for the request that was refused.
func (m *meter) toldByServer(tokens, sentBytes int) {
	if tokens <= 0 || sentBytes <= 0 {
		return
	}
	m.anchorTokens, m.anchorBytes = tokens, sentBytes
	if ratio := float64(sentBytes) / float64(tokens); ratio >= minBytesPerTok && ratio <= maxBytesPerTok {
		m.bytesPerTok = ratio
	}
}

// shrinkAnchor takes out of the anchor a piece of the conversation the model already read. The count
// the server gave stays true for the part that is still being sent, so a fold or a drop keeps an exact
// number instead of throwing it away: zeroing it sent every later estimate back to a byte ratio over
// the whole conversation, which is where the error of the guess comes from.
func (m *meter) shrinkAnchor(bytes int) {
	if m.anchorTokens <= 0 || bytes <= 0 {
		return
	}
	gone := int(float64(bytes) / max(m.bytesPerTok, minBytesPerTok))
	m.anchorBytes = max(m.anchorBytes-bytes, 0)
	m.anchorTokens = max(m.anchorTokens-gone, 0)
}

// publishUsage writes down what the conversation costs, once, at the end of a turn. A front
// end reading it while the next turn runs on another goroutine never walks the history.
func (a *Agent) publishUsage() {
	est := a.estimateTokens()
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	a.report = Usage{
		Turns:       a.meter.turns,
		PromptTok:   a.meter.prompt,
		CachedTok:   a.meter.cached,
		GenTok:      a.meter.generated,
		TotalPrompt: a.meter.totalPrompt,
		CachedTotal: a.meter.cachedTotal,
		EstTok:      est,
		BytesPerTok: a.meter.bytesPerTok,
		Draft:       a.meter.draft,
		DraftOK:     a.meter.draftOK,
		Acceptance:  acceptance(a.meter.draftOK, a.meter.draft),
	}
}

// Usage reports the numbers as of the last finished turn.
func (a *Agent) Usage() Usage {
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	return a.report
}

// ContextLine answers /ctx: how full the window is, how much of it the server already had,
// what the tool descriptions cost, and how well a draft model is doing where there is one.
func (a *Agent) ContextLine() string {
	u := a.Usage()
	var b strings.Builder
	if a.opts.CtxSize > 0 {
		fmt.Fprintf(&b, "window %d tokens · ~%d used (%d%%) · tool descriptions %d bytes\n",
			a.opts.CtxSize, u.EstTok, u.EstTok*100/a.opts.CtxSize, a.SpecBytes())
	} else {
		fmt.Fprintf(&b, "window unknown (set --ctx) · tool descriptions %d bytes\n", a.SpecBytes())
	}
	if u.Turns > 0 {
		fmt.Fprintf(&b, "last request %d tokens, %d of them cached (%d%%)\n",
			u.PromptTok, u.CachedTok, percent(u.CachedTok, u.PromptTok))
		fmt.Fprintf(&b, "%d prompt tokens in all, %d of them served from the cache\n", u.TotalPrompt, u.CachedTotal)
	}
	if a.opts.MaxResult > 0 {
		fmt.Fprintf(&b, "one tool result is capped at %d bytes\n", a.opts.MaxResult)
	}
	if u.Acceptance > 0 {
		fmt.Fprintf(&b, "speculative decoding: %d%% of the draft's tokens in the last reply were kept (%d of %d)",
			int(u.Acceptance*100+0.5), u.DraftOK, u.Draft)
	}
	return b.String()
}
