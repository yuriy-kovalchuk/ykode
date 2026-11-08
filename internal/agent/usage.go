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
	Draft       int // tokens a draft model proposed, where the server runs one
	DraftOK     int // of those, what the model kept
	Acceptance  float64
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
	if ratio := float64(sentBytes) / float64(stats.PromptTokens); ratio >= minBytesPerTok && ratio <= maxBytesPerTok {
		m.bytesPerTok = ratio
	}
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
		fmt.Fprintf(&b, "speculative decoding: %d%% of the draft's tokens were kept (%d of %d)",
			int(u.Acceptance*100+0.5), u.DraftOK, u.Draft)
	}
	return b.String()
}
