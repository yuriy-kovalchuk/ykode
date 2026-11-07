package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

const (
	compactKeep   = 2               // the newest tool calls stay in full
	foldGap       = compactKeep + 4 // new calls between folds: each fold costs a request and the whole prompt cache
	compactPrompt = `Summarise the earlier part of a coding session so it can be dropped from the context.
Answer in four short sections - Goal, Files touched, What was decided, Next step.
Only state what the transcript shows. Under 150 words.`
	compactMessage   = "[ykode compacted the earlier turns into this summary]"
	compactLineCap   = 300  // bytes of one message in the transcript handed to the summariser
	compactMaxTokens = 400  // the summary is meant to be short
	maxSummaryChars  = 2000 // a runaway summary is cut, not kept
)

// Compact replaces the older turns with a summary the model writes, keeping the newest calls intact.
// It returns the tokens freed; if the summary cannot be made, it falls back to shortening tool results.
func (a *Agent) Compact(ctx context.Context) int {
	before := a.estimateTokens()
	start, task := a.compactCut()
	if start < 2 {
		a.shrinkForCompaction()
		a.trimHistory()
		return before - a.estimateTokens()
	}
	summary, err := a.summariseWithModel(ctx, a.transcriptOf(a.dropping(start, task)))
	if err != nil {
		a.tell("the model could not summarise the older turns; shortening tool results instead")
		a.shrinkForCompaction()
		a.trimHistory()
		return before - a.estimateTokens()
	}
	dropped := a.dropping(start, task)
	a.forget(dropped...)
	kept := []llm.Message{{Role: "user", Content: compactMessage + "\n" + summary}}
	if task > 0 {
		// the job in hand is kept word for word: a summary is for what happened, and a model
		// working from a paraphrase of the request starts answering a question nobody asked
		kept = append(kept, a.history[task])
	}
	a.history = append(a.history[:1], append(kept, a.history[start:]...)...)
	a.compactedAt = callBlocks(a.history)
	return before - a.estimateTokens()
}

// callBlocks counts the tool-call blocks in a conversation: the unit compaction folds.
func callBlocks(messages []llm.Message) int {
	blocks := 0
	for _, m := range messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			blocks++
		}
	}
	return blocks
}

// foldOverdue says enough calls have piled up since the last fold to be worth another one. A long
// run empties the window more than once, and by the second time there are no old results left to
// shorten, so folding the turns again is the only thing that earns its request.
func (a *Agent) foldOverdue() bool {
	return a.compactedAt < 0 || callBlocks(a.history)-a.compactedAt >= foldGap
}

// dropping is what the summary replaces: everything before the kept calls except the message
// compactCut picked out to survive in its own words.
func (a *Agent) dropping(start, task int) []llm.Message {
	if task <= 0 {
		return a.history[1:start]
	}
	return append(append([]llm.Message{}, a.history[1:task]...), a.history[task+1:start]...)
}

// shrinkForCompaction shortens every old result but leaves the newest calls whole: those turns
// survive the summary, and the model still needs what they said and what they wrote.
func (a *Agent) shrinkForCompaction() {
	a.shrinkOldResults(0)
	a.shrinkOldEdits(compactKeep)
}

// canCompact says there are enough tool calls to fold into a summary.
func (a *Agent) canCompact() bool {
	return callBlocks(a.history) > compactKeep
}

// compactCut picks where the kept part starts: the beginning of the second newest tool call, so a
// call and its replies always move together. It also picks the newest user message in the part being
// folded away, which is kept for its wording rather than summarised.
// The second value is the index of that message, or 0 when the folded part holds none.
func (a *Agent) compactCut() (start, task int) {
	var blocks []int
	for i, m := range a.history {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			blocks = append(blocks, i)
		}
	}
	if len(blocks) <= compactKeep {
		return 0, 0
	}
	start = blocks[len(blocks)-compactKeep]
	if first := blocks[0]; first < 2 {
		return 0, 0
	}
	for i := start - 1; i > 0; i-- {
		if a.history[i].Role == "user" && a.history[i].Content == a.task { // ykode's own notes are user messages too
			return start, i
		}
	}
	return start, 0
}

// transcriptOf flattens messages into the text the summariser is asked about.
func (a *Agent) transcriptOf(messages []llm.Message) string {
	var b strings.Builder
	for _, m := range messages {
		switch {
		case m.Role == "tool":
			fmt.Fprintf(&b, "%s %s -> %s\n", m.CallID, m.Name, oneLine(m.Content, compactLineCap))
		case len(m.ToolCalls) > 0:
			for _, call := range m.ToolCalls {
				fmt.Fprintf(&b, "call %s %s\n", call.Function.Name, oneLine(string(call.Function.Arguments), compactLineCap))
			}
		default:
			fmt.Fprintf(&b, "%s: %s\n", m.Role, oneLine(m.Content, compactLineCap))
		}
	}
	if a.opts.CtxSize <= 0 {
		return b.String()
	}
	// the summary request must fit the window too: 40% of it, keeping the start and the latest work
	return tools.FormatResult(b.String(), int(float64(a.opts.CtxSize*2/5)*a.meter.bytesPerTok))
}

// summariseWithModel asks for the summary in its own request: no tools, a small answer, no history tailoring.
func (a *Agent) summariseWithModel(ctx context.Context, transcript string) (string, error) {
	if transcript == "" {
		return "", fmt.Errorf("nothing to summarise")
	}
	turn, err := a.client.ChatStream(ctx, llm.ChatRequest{
		Model: a.model,
		Messages: []llm.Message{
			{Role: "system", Content: compactPrompt},
			{Role: "user", Content: transcript},
		},
		MaxTokens: compactMaxTokens,
		// a summary needs no thinking phase, and thinking costs the window we are trying to free
		ChatTemplateKwargs: map[string]any{"enable_thinking": false},
	}, nil)
	if err != nil {
		return "", err
	}
	summary := strings.TrimSpace(turn.Message.Content)
	if summary == "" {
		return "", fmt.Errorf("the model answered with nothing to keep")
	}
	return oneLine(summary, maxSummaryChars), nil
}
