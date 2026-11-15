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
	SendReasoning    bool         // repeat earlier thinking to templates that expect it back
	MaxResult        int          // byte cap on one tool result; 0 means the tools' own default
	Session          *session.Log // append-only copy of the conversation, for --continue
	Root             string       // the project root, recorded with every call
	Allow            []string     // what never to ask about: a tool name, or "exec:go test"
	Plan             *tools.Plan  // the steps of a long task: the model writes them, a front end shows them
	// Cards are the per-model recommendations a settings file gave, for every model it named and not
	// only the one the run started on: sampling, and whether that template wants earlier thinking sent
	// back. Without them a switch moves the window and the model id and keeps the old model's numbers.
	Cards map[string]llm.ModelCard
}

// SetEndpoint points the next request at another server and model, with that model's window. A request
// in flight is reading the client, the model and the window to build itself, so it refuses while one
// runs rather than trusting the caller to have checked.
func (a *Agent) SetEndpoint(client *llm.Client, model string, ctxSize int) bool {
	if a.refuseDuringATurn("switching server") {
		return false
	}
	a.client, a.model, a.opts.CtxSize = client, model, ctxSize
	if a.opts.KeepRecentTokens > 0 {
		a.opts.KeepRecentTokens = min(20000, ctxSize/5)
	}
	// the sampling and the thinking-repeat came from a model card resolved at start-up. A switch to a
	// model that has its own card takes that card; one with no card keeps what is in use, because
	// nothing is known about it and the server's own default is not necessarily what was working.
	if card, known := a.opts.Cards[model]; known {
		a.opts.Thinking, a.opts.NoThink, a.opts.SendReasoning = card.Thinking, card.NoThink, card.SendReasoning
	}
	a.publishUsage() // the footer's window is the new one
	return true
}

// Tune changes options between turns, for a setting changed while the session runs. The round-trip in
// flight is reading these options to build its request, so this refuses while one runs: the rule used to
// be a comment and a promise about who called it.
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

// Branch names what HEAD pointed at when the prompt was built. The screen refreshes it before each
// ask; this saves the run one git call before anything is on screen.
func (a *Agent) Branch() string { return a.branch }

// Model is the model id this agent talks to.
func (a *Agent) Model() string { return a.model }

// Plan is the steps of the task the run is working through, as the model last wrote them; nil when
// it wrote none. The run writes them while a front end reads them, so this is a copy.
func (a *Agent) Plan() []tools.PlanStep { return a.opts.Plan.Steps() }

// SavedMessages counts the conversation this run wrote to its session file.
func (a *Agent) SavedMessages() int { return a.opts.Session.Count() }

// SessionFile names the conversation this run is writing, "" when it saves none; it moves when the run
// comes back to another one, so a front end's "come back with" line cannot point at the wrong file.
func (a *Agent) SessionFile() string { return a.opts.Session.Path() }
