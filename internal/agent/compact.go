package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

const (
	compactKeep   = 2 // the newest tool calls stay in full
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
	start, transcript := a.compactCut()
	if start < 2 {
		a.shrinkOldResults(0)
		a.trimHistory()
		return before - a.estimateTokens()
	}
	summary, err := a.summariseWithModel(ctx, transcript)
	if err != nil {
		a.tell("the model could not summarise the older turns; shortening tool results instead")
		a.shrinkOldResults(0)
		a.trimHistory()
		return before - a.estimateTokens()
	}
	a.history = append(a.history[:1], append([]llm.Message{{
		Role:    "user",
		Content: compactMessage + "\n" + summary,
	}}, a.history[start:]...)...)
	a.shortened = true
	return before - a.estimateTokens()
}

// canCompact says there are enough tool calls to fold into a summary.
func (a *Agent) canCompact() bool {
	blocks := 0
	for _, m := range a.history {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			blocks++
		}
	}
	return blocks > compactKeep
}

// compactCut picks where the kept part starts: the beginning of the second newest tool call, so a
// call and its replies always move together. It also returns the transcript of what is being dropped.
func (a *Agent) compactCut() (int, string) {
	var blocks []int
	for i, m := range a.history {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			blocks = append(blocks, i)
		}
	}
	if len(blocks) <= compactKeep {
		return 0, ""
	}
	start := blocks[len(blocks)-compactKeep]
	if first := blocks[0]; first < 2 {
		return 0, ""
	}
	return start, a.transcriptOf(a.history[1:start])
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
	return b.String()
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
