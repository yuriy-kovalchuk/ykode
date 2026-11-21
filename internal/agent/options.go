package agent

import (
	"cmp"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// Options configures one agent.
type Options struct {
	MaxTurns         int
	MaxTokens        int          // cap on one response
	KeepRecentTokens int          // what a fold leaves of the newest work, at least; 0 keeps only the newest calls
	SafetyMargin     int          // tokens left free for the chat template; 0 means ctxSafetyTok
	CtxPinned        bool         // --ctx fixed the window: a model switch keeps it
	CtxSize          int          // server context window; 0 = unknown, no budgeting
	ThinkingLevel    string       // off, minimal, low, medium, high, xhigh, max; empty is medium
	Thinking         llm.Sampling // sent while the model thinks
	NoThink          llm.Sampling // sent with thinking off; Qwen's cards recommend different values
	AssumeYes        bool
	Instructions     string       // what the project's AGENTS.md / CLAUDE.md said
	MaxResult        int          // byte cap on one tool result; 0 means the tools' own default
	Session          *session.Log // append-only copy of the conversation, for --continue
	Root             string       // the project root, recorded with every call
	Vision           bool         // the model in use reads images, so a picture read from a file is worth sending
	Allow            []string     // what never to ask about: a tool name, or "exec:go test"
	Disabled         []string     // tools the model is never offered, taken out before the first request
	Plan             *tools.Plan  // the steps of a long task: the model writes them, a front end shows them
	// Cards are the per-model sampling a settings file gave, so a switch does not keep old numbers
	Cards map[string]llm.ModelCard
}

// KeepRecentTokens is what a fold keeps of recent work: pi's 20,000, or a fifth of a smaller window
func KeepRecentTokens(ctxSize int) int { return min(keepRecentMax, ctxSize/5) }

const keepRecentMax = 20000

// SetEndpoint points the next request at another server, refusing while a round-trip is in flight
func (a *Agent) SetEndpoint(client *llm.Client, model llm.ModelInfo, ctxSize int) bool {
	if a.refuseDuringATurn("switching server") {
		return false
	}
	a.client, a.model, a.opts.CtxSize, a.opts.Vision = client, model.ID, ctxSize, model.Vision
	a.opts.KeepRecentTokens = KeepRecentTokens(ctxSize)
	// a model with no card keeps the sampling in use: the server's default is not necessarily better
	if card, known := a.opts.Cards[model.ID]; known {
		a.opts.Thinking, a.opts.NoThink = card.Thinking, card.NoThink
	}
	a.publishUsage() // the footer's window is the new one
	return true
}

// Tune changes options between turns, refusing while a round-trip reads them to build its request
func (a *Agent) Tune(change func(*Options)) bool {
	if a.refuseDuringATurn("changing a setting") {
		return false
	}
	change(&a.opts)
	return true
}

// SetModel changes what the next request asks for; the server may still be loading it.
func (a *Agent) SetModel(id string) bool {
	if a.refuseDuringATurn("switching model") {
		return false
	}
	a.model = id
	return true
}

// SetThink decides whether the model gets a thinking phase on the turns that follow.
func (a *Agent) SetThinkingLevel(level string) bool {
	if a.refuseDuringATurn("changing the thinking level") {
		return false
	}
	a.opts.ThinkingLevel = level
	return true
}

// ThinkingLevel is how deeply the model is allowed to think before it answers.
func (a *Agent) ThinkingLevel() string { return cmp.Or(a.opts.ThinkingLevel, llm.DefaultThinkingLevel) }

// CtxSize is the context window the budget is measured against; 0 when the server never reported one.
func (a *Agent) CtxSize() int { return a.opts.CtxSize }

// Root is the project directory this session works in.
func (a *Agent) Root() string { return a.opts.Root }

// Branch names what HEAD pointed at when the prompt was built, saving one git call per ask
func (a *Agent) Branch() string { return a.branch }

// Model is the model id this agent talks to.
func (a *Agent) Model() string { return a.model }

// Plan is the steps the model last wrote, nil if none; a copy, since the run writes them
func (a *Agent) Plan() []tools.PlanStep { return a.opts.Plan.Steps() }

// SavedMessages counts the conversation this run wrote to its session file.
func (a *Agent) SavedMessages() int { return a.opts.Session.Count() }

// SessionFile names the conversation this run writes, "" when it saves none, and moves on a resume
func (a *Agent) SessionFile() string { return a.opts.Session.Path() }
