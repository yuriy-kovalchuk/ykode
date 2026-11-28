package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
)

const (
	compactKeep = 2               // the newest tool calls always stay in full, one where that is all there is
	foldGap     = compactKeep + 4 // new calls between folds: each fold costs a request and the whole prompt cache
	// the summary is the only memory of the folded turns, so it is told what it is for and what shape
	compactPrompt = `The conversation above is about to leave your context window. Write the memory you will carry on from: the summary below is all that is kept of it, plus the newest messages.

Write these sections in this order, in markdown. Write "none" under a section that has nothing.

## Goal
What the user is trying to get done, in their terms. Quote their wording where it is specific.

## Constraints and preferences
Rules, style, limits and things to avoid that the user or the project stated.

## Progress
### Done
What is finished and, where it matters, how it was checked.
### In progress
What was being worked on when the window filled.
### Blocked
What failed and why, with the error text that says so.

## Key decisions
One line each: what was chosen, and why.

## Critical context
Exact names the next step needs: paths, functions, types, commands that worked or failed, values, test names. Copy them verbatim.

## Next steps
Ordered and concrete: the first one is what the agent does next.

Rules:
- State only what the conversation shows. Do not guess, and do not invent paths or results.
- The thinking that went into a decision is not sent back to the model, and it goes away with the
  turns it came from: write down the conclusion that thinking reached whenever the conclusion is not
  visible in what was read, changed or run, under Key decisions.
- Facts, not narrative. Leave out greetings, apologies and anything the agent can re-read from a file.
- Do not list files: they are attached separately.
- The newest messages are kept word for word right after your summary. Describe the state at the end of what you are given, say "as of this point" under In progress and Next steps, and never claim that something still has to be done when you cannot tell.
- If the conversation above begins with an earlier summary, update it instead of starting over: keep what is still true, move finished work to Done, drop what no longer matters, add what changed since.

Write the summary now, starting with "## Goal". Do not call a tool.`
	compactMaxTokens = 1500 // room for the sections above
	maxSummaryChars  = 6000 // a runaway summary is cut, not kept
	maxListedFiles   = 60   // files one list names; the newest win
)

// FoldMark starts the recap a fold leaves behind, so a screen can tell it from what the user typed
const FoldMark = "[ykode compacted the earlier turns into this summary]"

// Compact replaces older turns with a summary the model writes, keeps the newest calls, and frees tokens
func (a *Agent) Compact(ctx context.Context) int {
	// a fold rewrites the conversation, so it owns the run unless a turn already owns it
	if owns := a.turnRunning.CompareAndSwap(false, true); owns {
		defer a.turnRunning.Store(false)
	}
	a.sanitizeHistory()
	before := a.estimateTokens()
	start, task := a.compactCut()
	if start < 2 {
		a.compactedAt = callBlocks(a.history) // the next try waits for new work, not for the next turn
		a.tell("too little older work to fold")
		return 0
	}
	summary, err := a.summarise(ctx, start)
	if err != nil {
		a.compactedAt = callBlocks(a.history)
		a.tell("the older turns could not be summarised, so they stay as they are: " + err.Error())
		return 0
	}
	dropped := a.dropping(start, task)
	if len(summary) >= historyBytes(dropped) {
		// the recap would be as big as what it replaces: folding would cost a request and save nothing
		a.compactedAt = callBlocks(a.history)
		a.tell("the older turns are already smaller than a summary of them: nothing was folded")
		return 0
	}
	a.noteFiles(dropped)
	kept := []llm.Message{{Role: "user", Content: FoldMark + "\n" + summary + a.fileBlocks() + a.planBlock()}}
	if task > 0 {
		// the job in hand is kept word for word: a summary is for what happened, not for the ask's wording
		kept = append(kept, a.history[task])
	}
	a.history = append(a.history[:1:1], append(kept, a.history[start:]...)...)
	a.recountBytes()
	a.compactedAt = callBlocks(a.history)
	a.meter.shrinkAnchor(historyBytes(dropped)) // the count the server gave still stands for what is left
	a.work.made(roomFolded(len(dropped)))
	a.emit(Event{Kind: Summary, Text: summary})
	a.reportSave(a.opts.Session.Folded(session.Fold{Kept: a.history[1:], Summary: summary,
		ReadFiles: a.readOnlyFiles(), ModifiedFiles: a.modifiedFiles, Dropped: len(dropped), TokensBefore: before}))
	a.publishUsage()
	return before - a.estimateTokens()
}

// callBlocks counts the tool-call blocks in a conversation: the unit a fold folds.
func callBlocks(messages []llm.Message) int {
	blocks := 0
	for _, m := range messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			blocks++
		}
	}
	return blocks
}

// foldOverdue says enough calls have piled up since the last fold to be worth another one
func (a *Agent) foldOverdue() bool {
	return a.compactedAt < 0 || callBlocks(a.history)-a.compactedAt >= foldGap
}

// dropping is what the summary replaces: all but the kept calls and the one ask kept verbatim
func (a *Agent) dropping(start, task int) []llm.Message {
	if task <= 0 {
		return a.history[1:start]
	}
	return append(append([]llm.Message{}, a.history[1:task]...), a.history[task+1:start]...)
}

// canCompact says there is a call to fold and one to keep: with one call, keep everything
func (a *Agent) canCompact() bool {
	return callBlocks(a.history) >= 2
}

// compactCut picks where the kept part starts, never between a call and its replies
func (a *Agent) compactCut() (start, task int) {
	var blocks []int
	for i, m := range a.history {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			blocks = append(blocks, i)
		}
	}
	if len(blocks) < 2 {
		return 0, 0
	}
	// a small window holds few big calls, so it keeps the newest one: a fold that frees something wins
	keep := compactKeep
	if len(blocks) <= compactKeep {
		keep = 1
	}
	start = blocks[len(blocks)-keep]
	if first := blocks[0]; first < 2 {
		return 0, 0
	}
	if budget := a.opts.KeepRecentTokens; budget > 0 {
		rate := max(a.meter.bytesPerTok, minBytesPerTok)
		kept := int(float64(historyBytes(a.history[start:])) / rate)
		reached := kept >= budget
		for i := start - 1; i >= 2 && !reached; i-- {
			kept += int(float64(historyBytes(a.history[i:i+1]))/rate) + 4
			if reached = kept >= budget; reached {
				for i > 2 && a.history[i].Role == "tool" {
					i-- // a result stays with the call that asked for it
				}
				start = i
			}
		}
		if !reached || start < 2 {
			return 0, 0 // everything older than the newest calls is less than the room asked for: nothing to fold
		}
	}
	for i := start - 1; i > 0; i-- {
		if a.history[i].Role == "user" && a.history[i].Content == a.task { // ykode's own notes are user messages too
			return start, i
		}
	}
	return start, 0
}

// summarise sends history[:start] exactly as before, plus one instruction, so the cache covers it
func (a *Agent) summarise(ctx context.Context, start int) (string, error) {
	prefix := a.history[:start]
	if a.opts.CtxSize > 0 {
		rate := max(a.meter.bytesPerTok, minBytesPerTok)
		sent := a.sendingBytes() - historyBytes(a.history[start:]) + len(compactPrompt)
		if int(float64(sent)/rate)+a.compactRoom() > a.usable() {
			return "", fmt.Errorf("they are larger than the window")
		}
	}
	messages := append(append([]llm.Message{}, prefix...), llm.Message{Role: "user", Content: compactPrompt})
	var why error
	// asked first as a normal request so the cached prefix matches, then with tools refused
	for _, choice := range []string{"auto", "none"} {
		turn, err := a.client.ChatStream(ctx, llm.ChatRequest{
			Model: a.model, Messages: messages, Tools: a.specs(), ToolChoice: choice,
			MaxTokens: a.compactRoom(), Sampling: a.opts.NoThink,
			// a summary needs no thinking phase, and its sampling comes from the model's own card
			ChatTemplateKwargs: map[string]any{"enable_thinking": false},
			// the turns' own word, since a template may write it into the prompt the cache matches on
			ReasoningEffort: a.effort(a.ThinkingLevel()),
		}, nil)
		if err != nil {
			return "", err
		}
		summary := keepSummary(turn.Message.Content)
		if len(turn.Message.ToolCalls) > 0 {
			why = fmt.Errorf("the model called a tool instead of writing the summary")
			continue
		}
		if why = checkSummary(summary); why == nil {
			a.tell(fmt.Sprintf("summary written: the server had %d of the %d prompt tokens cached",
				turn.Timings.CacheN, turn.Timings.PromptTokens()))
			return summary, nil
		}
	}
	return "", why
}

// compactRoom is what the summary may cost, shrunk in a small window so a fold stays possible
func (a *Agent) compactRoom() int {
	return min(compactMaxTokens, max(a.opts.CtxSize/8, minResponseTok))
}

// keepSummary caps a runaway summary without flattening the short blocks it was written in
func keepSummary(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxSummaryChars {
		return text
	}
	return strings.TrimRight(string([]rune(text)[:maxSummaryChars]), " \n") + "…"
}

// checkSummary refuses a reply that is not a summary, which would replace real context with noise
func checkSummary(summary string) error {
	switch {
	case summary == "":
		return fmt.Errorf("the model answered with nothing to keep")
	case hasTextCall(summary):
		return fmt.Errorf("the model wrote a tool call instead of a summary")
	case !strings.Contains(strings.ToLower(summary), "goal"):
		return fmt.Errorf("the model's reply is not in the summary's sections")
	}
	return nil
}
