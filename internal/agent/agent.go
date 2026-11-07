// Package agent runs the loop between the model and the tools until the model answers in text.
package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/yuriy-kovalchuk/ykode/internal/audit"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// finishLength is the finish reason a server sends when it stopped at max_tokens.
const finishLength = "length"

// Options configures one agent.
type Options struct {
	MaxTurns      int
	MaxTokens     int // cap on one response
	CtxSize       int // server context window; 0 = unknown, no budgeting
	Think         bool
	AssumeYes     bool
	Instructions  string            // what the project's AGENTS.md / CLAUDE.md said
	NoVerify      bool              // never push a text-only answer back into the tools
	SendReasoning bool              // repeat earlier thinking to templates that expect it back
	MaxResult     int               // byte cap on one tool result; 0 means the tools' own default
	Store         tools.ResultStore // where shortened tool results are kept for recall
	Audit         *audit.Logger     // append-only record of every tool call; nil turns it off
	Session       *session.Log      // append-only copy of the conversation, for --continue
	Root          string            // the project root, recorded with every call
	Allow         []string          // what never to ask about: a tool name, or "exec:go test"
	Undo          Undoer            // the stack /undo reads; wired where the tools are built
}

// Stats summarises one model round-trip for the user.
type Stats struct {
	FinishReason   string
	PromptTokens   int
	CachedTokens   int
	OutputTokens   int
	PrefillTokSec  float64
	DecodeTokSec   float64
	DraftN         int // tokens a draft model proposed, when the server has one
	DraftNAccepted int
}

type Agent struct {
	client        *llm.Client
	tools         *tools.Registry
	model         string
	opts          Options
	sink          Sink
	shortened     bool         // a tool result has been shortened, so recall is worth describing
	callSeq       int          // numbers the r1, r2... ids that recall and the audit log share
	compacted     bool         // the older turns were folded into a summary once already
	auditBroken   bool         // the audit log failed once: say so once, not per call
	perms         *permissions // what the user has already agreed to keep allowing
	sessionBroken bool         // the session log failed once: say so once, not per message
	turn          int          // the round-trip currently running, carried on every event
	history       []llm.Message
	meter         meter
	usageMu       sync.Mutex // the numbers a front end reads while a turn runs elsewhere
	report        Usage      // what the conversation cost as of the last finished turn
}

func New(client *llm.Client, registry *tools.Registry, model string, opts Options, sink Sink) *Agent {
	if sink == nil {
		sink = nullSink{}
	}
	a := &Agent{client: client, tools: registry, model: model, opts: opts, sink: sink}
	a.meter = newMeter()
	a.publishUsage()
	a.perms = newPermissions(opts.Allow)
	a.Reset()
	return a
}

// Reset keeps the system prompt and forgets the rest of the conversation; the learned byte ratio stays.
func (a *Agent) Reset() {
	a.history = []llm.Message{{Role: "system", Content: withInstructions(SystemPrompt, a.opts.Instructions)}}
	a.meter.clearCost()
	a.compacted, a.shortened = false, false
	a.publishUsage()
}

const verifyNote = "You answered without calling any tool. If your answer depends on this project's files, verify it now with find_files, search_text or read_file, then answer again."

// projectWorkRe marks requests where answering without reading is likely wrong; whole words, so "address" and "profile" stay out.
var projectWorkRe = regexp.MustCompile(`\b(?:fix|bug|test|build|compile|error|fail|refactor|implement|add|change|edit|read|write|code|function|file|review|why does|what does)\b|src/`)

func looksLikeProjectWork(prompt string) bool {
	return projectWorkRe.MatchString(strings.ToLower(prompt))
}

// Ask runs model round-trips until the model replies without a tool call; a cancelled context ends quietly.
func (a *Agent) Ask(ctx context.Context, prompt string) error {
	a.add(llm.Message{Role: "user", Content: prompt})

	usedTools, nudged, forceTool := false, false, false
	for turn := 1; ; turn++ {
		a.turn = turn
		a.emit(Event{Kind: TurnStart, Turn: turn})
		if a.underPressure() {
			a.shrinkOldResults(keepResults)
		}
		if a.stillTight() && !a.compacted && a.canCompact() {
			// shortening the results was not enough: pay one request to fold the older turns into a summary
			a.tell("the conversation is out of room: asking the model to summarise the older turns")
			a.compacted = true
			a.Compact(ctx)
		}
		a.trimHistory() // last resort: blank what is left, then drop whole calls
		req, err := a.request(forceTool)
		forceTool = false
		if err != nil {
			return err
		}
		msg, finish, err := a.chat(ctx, req)
		if err != nil {
			// Close any half-streamed thinking block before reporting.
			a.emit(Event{Kind: TurnEnd, Turn: turn})
			if ctx.Err() != nil {
				a.note("(the user cancelled this request)")
				return nil
			}
			return fmt.Errorf("chat turn %d: %w", turn, err)
		}
		a.add(msg)

		if finish == finishLength {
			a.tell("the response hit max_tokens and may be incomplete")
			if !toolCallsRunnable(msg.ToolCalls) {
				// a cut-off tool call cannot run: tell the model rather than hand broken arguments over
				a.note("(your reply was cut off by max_tokens and the tool call was not valid JSON; make it smaller or split it into two calls)")
				continue
			}
		}
		if len(msg.ToolCalls) == 0 {
			if strings.TrimSpace(msg.Content) == "" {
				a.tell("the model returned no text")
			}
			if !nudged && !usedTools && !a.opts.NoVerify && turn < a.opts.MaxTurns && looksLikeProjectWork(prompt) {
				nudged, forceTool = true, true
				a.tell("nothing was read for this request; asking once to verify with a tool")
				a.add(llm.Message{Role: "user", Content: verifyNote})
				continue
			}
			return nil
		}

		usedTools = true
		a.runToolCalls(ctx, msg.ToolCalls)
		if turn >= a.opts.MaxTurns {
			a.note(fmt.Sprintf("(stopped after %d tool rounds without a final answer)", a.opts.MaxTurns))
			return fmt.Errorf("reached --max-turns (%d) before the model answered", a.opts.MaxTurns)
		}
	}
}

func (a *Agent) chat(ctx context.Context, req llm.ChatRequest) (llm.Message, string, error) {
	turn, err := a.client.ChatStream(ctx, req, func(d llm.Delta) {
		if d.Reasoning != "" {
			a.emit(Event{Kind: Thinking, Text: d.Reasoning})
		}
		if d.Content != "" {
			a.emit(Event{Kind: Answer, Text: d.Content})
		}
	})
	if err != nil {
		return llm.Message{}, "", err
	}
	stats := Stats{
		FinishReason:   turn.FinishReason,
		PromptTokens:   turn.Timings.PromptTokens(),
		CachedTokens:   turn.Timings.CacheN,
		OutputTokens:   turn.Timings.PredictedN,
		PrefillTokSec:  turn.Timings.PromptPerSecond,
		DecodeTokSec:   turn.Timings.PredictedPerSecond,
		DraftN:         turn.Timings.DraftN,
		DraftNAccepted: turn.Timings.DraftNAccepted,
	}
	a.meter.observe(stats, a.sendingBytes())
	a.publishUsage()
	a.emit(Event{Kind: TurnEnd, Turn: a.turn, Stats: stats})
	msg := repair(turn.Message)
	if a.opts.SendReasoning {
		msg.WireReasoning = msg.Reasoning
	}
	return msg, turn.FinishReason, nil
}

// History reports what this run's audit log says about the calls it made.
// History answers /history: a tool name, "failed", "outside", an id like r12, or --all to
// widen the scope from this run to every run of this root.
func (a *Agent) History(query string, limit int) string {
	if a.opts.Audit == nil {
		return "this run has no audit log"
	}
	query, all := splitAll(query)
	entries, err := a.logged(all)
	if err != nil {
		return err.Error()
	}
	if len(entries) == 0 {
		return "nothing has been logged yet"
	}
	return audit.Answer(entries, query, limit)
}

// splitAll takes --all out of the query, wherever the user put it.
func splitAll(query string) (string, bool) {
	fields := strings.Fields(query)
	kept := fields[:0]
	all := false
	for _, f := range fields {
		if f == "--all" {
			all = true
			continue
		}
		kept = append(kept, f)
	}
	return strings.Join(kept, " "), all
}

func (a *Agent) logged(all bool) ([]audit.Entry, error) {
	if all {
		return a.opts.Audit.ReadRoot(a.opts.Root)
	}
	return a.opts.Audit.Read(a.opts.Audit.Session())
}

// tell reports something the run did that is not a tool call.
// add extends the conversation and, when one is configured, the on-disk copy of it.
// SetModel changes what the next request asks for; the server may still be loading it.
func (a *Agent) SetModel(id string) { a.model = id }

// SetThink decides whether the model gets a thinking phase on the turns that follow.
func (a *Agent) SetThink(on bool) { a.opts.Think = on }

// Thinking says whether the model is allowed to think out loud.
func (a *Agent) Thinking() bool { return a.opts.Think }

func (a *Agent) add(m llm.Message) {
	a.history = append(a.history, m)
	if err := a.opts.Session.Append(m); err != nil && !a.sessionBroken {
		a.sessionBroken = true // a session that will not save is reported once, not per message
		a.tell(err.Error())
	}
}

// Resume continues where a saved conversation stopped.
func (a *Agent) Resume(messages []llm.Message) {
	a.history = append(a.history, messages...)
	a.sanitizeHistory()
	a.trimHistory()
	a.publishUsage()
}

func (a *Agent) tell(text string) { a.emit(Event{Kind: Notice, Turn: a.turn, Text: text}) }

func (a *Agent) emit(ev Event) Decision {
	if ev.Turn == 0 {
		ev.Turn = a.turn
	}
	return a.sink.Emit(ev)
}

// note keeps the history honest when a turn ends without an answer.
func (a *Agent) note(text string) {
	a.add(llm.Message{Role: "assistant", Content: text})
}
