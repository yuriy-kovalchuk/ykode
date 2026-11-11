package agent

import (
	"cmp"

	"github.com/yuriy-kovalchuk/ykode/internal/audit"
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
	Instructions     string            // what the project's AGENTS.md / CLAUDE.md said
	NoVerify         bool              // never push a text-only answer back into the tools
	SendReasoning    bool              // repeat earlier thinking to templates that expect it back
	MaxResult        int               // byte cap on one tool result; 0 means the tools' own default
	Store            tools.ResultStore // where shortened command output is kept for the model to grep
	Audit            *audit.Logger     // append-only record of every tool call; nil turns it off
	Session          *session.Log      // append-only copy of the conversation, for --continue
	Root             string            // the project root, recorded with every call
	Allow            []string          // what never to ask about: a tool name, or "exec:go test"
	Undo             Undoer            // the stack /undo reads; wired where the tools are built
	Plan             *tools.Plan       // the steps of a long task: the model writes them, a front end shows them
}

// SetEndpoint points the next request at another server and model, with that model's window. It is called
// between turns, never during one, so nothing holds the old client.
func (a *Agent) SetEndpoint(client *llm.Client, model string, ctxSize int) {
	a.client, a.model, a.opts.CtxSize = client, model, ctxSize
	if a.opts.KeepRecentTokens > 0 {
		a.opts.KeepRecentTokens = min(20000, ctxSize/5)
	}
	a.publishUsage() // the footer's window is the new one
}

// Tune changes options between turns, for a setting changed while the session runs. Nothing reads the
// options while a turn is in flight, because the screen only calls this when none is.
func (a *Agent) Tune(change func(*Options)) { change(&a.opts) }

// SetModel changes what the next request asks for; the server may still be loading it.
func (a *Agent) SetModel(id string) { a.model = id }

// SetThink decides whether the model gets a thinking phase on the turns that follow.
func (a *Agent) SetThinkingLevel(level string) { a.opts.ThinkingLevel = level }

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
