package agent

import (
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// Usage is what the conversation has cost, in the server's own numbers.
type Usage struct {
	Turns       int
	PromptTok   int // prompt tokens of the last request
	CachedTok   int // of those, served from the server's prompt cache
	GenTok      int // tokens generated over the whole conversation
	TotalPrompt int // prompt tokens over the whole conversation
	EstTok      int // estimate for the request that would go out next
	BytesPerTok float64
}

const (
	defaultBytesPerTok = 3.3
	minBytesPerTok     = 1.5 // a ratio this far off means the server told us something else
	maxBytesPerTok     = 8.0
)

// meter adds up the turns and learns the byte ratio the server counts with.
type meter struct {
	turns, prompt, cached, generated, totalPrompt int
	bytesPerTok                                   float64
}

func newMeter() meter { return meter{bytesPerTok: defaultBytesPerTok} }

func (m *meter) clearCost() {
	m.turns, m.prompt, m.cached, m.generated, m.totalPrompt = 0, 0, 0, 0, 0
}

// observe folds one turn in; a server that sends no timings only leaves the counters at zero.
func (m *meter) observe(stats Stats, sentBytes int) {
	m.turns++
	m.prompt, m.cached = stats.PromptTokens, stats.CachedTokens
	m.generated += stats.OutputTokens
	m.totalPrompt += stats.PromptTokens
	if stats.PromptTokens <= 0 || sentBytes <= 0 {
		return
	}
	if ratio := float64(sentBytes) / float64(stats.PromptTokens); ratio >= minBytesPerTok && ratio <= maxBytesPerTok {
		m.bytesPerTok = ratio
	}
}

// Usage reports the cost of the conversation and the size of the next request.
func (a *Agent) Usage() Usage {
	return Usage{
		Turns:       a.meter.turns,
		PromptTok:   a.meter.prompt,
		CachedTok:   a.meter.cached,
		GenTok:      a.meter.generated,
		TotalPrompt: a.meter.totalPrompt,
		EstTok:      a.estimateTokens(),
		BytesPerTok: a.meter.bytesPerTok,
	}
}

// CtxSize is the context window the budget is measured against; 0 when the server never reported one.
func (a *Agent) CtxSize() int { return a.opts.CtxSize }

// Model is the model id this agent talks to.
func (a *Agent) Model() string { return a.model }

// sendingBytes counts every byte a request carries; the system prompt already sits at the head of history.
func (a *Agent) sendingBytes() int {
	bytes := 0
	for _, s := range a.specs() {
		bytes += len(s.Type) + len(s.Function.Name) + len(s.Function.Description) + len(s.Function.Parameters)
	}
	bytes += historyBytes(a.history)
	return bytes
}

func historyBytes(history []llm.Message) int {
	bytes := 0
	for _, m := range history {
		bytes += len(m.Content) + len(m.Name) + len(m.ToolCallID) + len(m.WireReasoning)
		for _, c := range m.ToolCalls {
			bytes += len(c.ID) + len(c.Function.Name) + len(c.Function.Arguments)
		}
	}
	return bytes
}
