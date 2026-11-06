// Package agent runs the loop between the model and the tools until the model answers in text.
package agent

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// finishLength is the finish reason a server sends when it stopped at max_tokens.
const finishLength = "length"

// ConfirmFunc asks the user whether an action may run; the CLI supplies it.
type ConfirmFunc func(what string) bool

// Options configures one agent.
type Options struct {
	MaxTurns      int
	MaxTokens     int // cap on one response
	CtxSize       int // server context window; 0 = unknown, no budgeting
	Think         bool
	AssumeYes     bool
	NoVerify      bool // never push a text-only answer back into the tools
	SendReasoning bool // repeat earlier thinking to templates that expect it back
	Confirm       ConfirmFunc
}

// Stats summarises one model round-trip for the user.
type Stats struct {
	FinishReason  string
	PromptTokens  int
	CachedTokens  int
	OutputTokens  int
	PrefillTokSec float64
	DecodeTokSec  float64
}

// Sink renders streamed output, with thinking and answer arriving apart.
type Sink interface {
	Reasoning(chunk string)
	Content(chunk string)
	TurnDone(Stats)
}

// quietSink discards the stream, for callers with no terminal.
type quietSink struct{}

func (quietSink) Reasoning(string) {}

func (quietSink) Content(string) {}

func (quietSink) TurnDone(Stats) {}

type Agent struct {
	client  *llm.Client
	tools   *tools.Registry
	model   string
	opts    Options
	sink    Sink
	log     io.Writer
	history []llm.Message
	meter   meter
}

func New(client *llm.Client, registry *tools.Registry, model string, opts Options, sink Sink, log io.Writer) *Agent {
	if opts.Confirm == nil {
		opts.Confirm = func(string) bool { return false }
	}
	if sink == nil {
		sink = quietSink{}
	}
	a := &Agent{client: client, tools: registry, model: model, opts: opts, sink: sink, log: log}
	a.meter = newMeter()
	a.Reset()
	return a
}

// Reset keeps the system prompt and forgets the rest of the conversation; the learned byte ratio stays.
func (a *Agent) Reset() {
	a.history = []llm.Message{{Role: "system", Content: SystemPrompt}}
	a.meter.clearCost()
}

const verifyNote = "You answered without calling any tool. If your answer depends on this project's files, verify it now with list_dir, find_files, search_text or read_file, then answer again."

// projectWorkRe marks requests where answering without reading is likely wrong; whole words, so "address" and "profile" stay out.
var projectWorkRe = regexp.MustCompile(`\b(?:fix|bug|test|build|compile|error|fail|refactor|implement|add|change|edit|read|write|code|function|file|review|why does|what does)\b|src/`)

func looksLikeProjectWork(prompt string) bool {
	return projectWorkRe.MatchString(strings.ToLower(prompt))
}

// Ask runs model round-trips until the model replies without a tool call; a cancelled context ends quietly.
func (a *Agent) Ask(ctx context.Context, prompt string) error {
	a.history = append(a.history, llm.Message{Role: "user", Content: prompt})

	usedTools, nudged, forceTool := false, false, false
	for turn := 1; ; turn++ {
		a.trimHistory()
		req, err := a.request(forceTool)
		forceTool = false
		if err != nil {
			return err
		}
		msg, finish, err := a.chat(ctx, req)
		if err != nil {
			// Close any half-streamed thinking block before reporting.
			a.sink.TurnDone(Stats{})
			if ctx.Err() != nil {
				a.note("(the user cancelled this request)")
				return nil
			}
			return fmt.Errorf("chat turn %d: %w", turn, err)
		}
		a.history = append(a.history, msg)

		if finish == finishLength {
			fmt.Fprintln(a.log, "  !! the response hit max_tokens and may be incomplete")
			if !toolCallsRunnable(msg.ToolCalls) {
				// a cut-off tool call cannot run: tell the model rather than hand broken arguments over
				a.note("(your reply was cut off by max_tokens and the tool call was not valid JSON; make it smaller or split it into two calls)")
				continue
			}
		}
		if len(msg.ToolCalls) == 0 {
			if strings.TrimSpace(msg.Content) == "" {
				fmt.Fprintln(a.log, "(the model returned no text)")
			}
			if !nudged && !usedTools && !a.opts.NoVerify && turn < a.opts.MaxTurns && looksLikeProjectWork(prompt) {
				nudged, forceTool = true, true
				fmt.Fprintln(a.log, "  .. nothing was read for this request; asking once to verify with a tool")
				a.history = append(a.history, llm.Message{Role: "user", Content: verifyNote})
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
			a.sink.Reasoning(d.Reasoning)
		}
		if d.Content != "" {
			a.sink.Content(d.Content)
		}
	})
	if err != nil {
		return llm.Message{}, "", err
	}
	stats := Stats{
		FinishReason:  turn.FinishReason,
		PromptTokens:  turn.Timings.PromptTokens(),
		CachedTokens:  turn.Timings.CacheN,
		OutputTokens:  turn.Timings.PredictedN,
		PrefillTokSec: turn.Timings.PromptPerSecond,
		DecodeTokSec:  turn.Timings.PredictedPerSecond,
	}
	a.meter.observe(stats, a.sendingBytes())
	a.sink.TurnDone(stats)
	msg := repair(turn.Message)
	if a.opts.SendReasoning {
		msg.WireReasoning = msg.Reasoning
	}
	return msg, turn.FinishReason, nil
}

// specs describes the tools to the model, in registration order.
func (a *Agent) specs() []llm.ToolSpec {
	if a.tools == nil { // a history-only agent, as the budget tests build one
		return nil
	}
	all := a.tools.All()
	specs := make([]llm.ToolSpec, 0, len(all))
	for _, t := range all {
		specs = append(specs, llm.ToolSpec{
			Type:     "function",
			Function: llm.ToolFunction{Name: t.Name(), Description: t.Description(), Parameters: t.Schema()},
		})
	}
	return specs
}

// note keeps the history honest when a turn ends without an answer.
func (a *Agent) note(text string) {
	a.history = append(a.history, llm.Message{Role: "assistant", Content: text})
}
