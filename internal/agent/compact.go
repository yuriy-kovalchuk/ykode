package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

const (
	compactKeep = 2               // the newest tool calls always stay in full
	foldGap     = compactKeep + 4 // new calls between folds: each fold costs a request and the whole prompt cache
	// the summary is the only memory of everything folded away, so it is told what that memory is for
	// and what shape to keep it in. A fold after the first updates the last summary instead of
	// starting over, so what was decided early survives any number of folds.
	compactPrompt = `You write the memory of a coding agent whose context window is full. The agent will carry on the work from your summary alone, plus the files listed beside it and the newest messages, so what you leave out is lost.

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
- State only what the transcript shows. Do not guess, and do not invent paths or results.
- Facts, not narrative. Leave out greetings, apologies and anything the agent can re-read from a file.
- Do not list files: they are attached separately.
- The newest messages are kept word for word right after your summary. Describe the state at the end of what you are given, say "as of this point" under In progress and Next steps, and never claim that something still has to be done when you cannot tell.
- If a <previous-summary> is given, update it instead of starting over: keep what is still true, move finished work to Done, drop what no longer matters, and add what the new turns changed.`
	compactAsk       = "Write the summary now, in the sections above, starting with \"## Goal\". Do not call tools and do not continue the conversation."
	compactLineCap   = 2000 // bytes of one message in the transcript handed to the summariser
	compactArgCap    = 200  // bytes of one argument of a call in that transcript
	compactMaxTokens = 1500 // room for the sections above
	maxSummaryChars  = 6000 // a runaway summary is cut, not kept
	maxListedFiles   = 60   // files one list names; the newest win
)

// FoldMark starts the message a fold leaves in the conversation: the recap of what was put away. A
// front end that draws a whole conversation reads it to tell a recap from something the user typed.
const FoldMark = "[ykode compacted the earlier turns into this summary]"

// Compact replaces the older turns with a summary the model writes, keeping the newest calls intact.
// It returns the tokens freed; if the summary cannot be made, it falls back to shortening tool results.
func (a *Agent) Compact(ctx context.Context) int {
	before := a.estimateTokens()
	start, task := a.compactCut()
	if start < 2 {
		a.tell("too few turns to fold: the older tool results were shortened instead")
		a.shrinkForCompaction()
		a.trimHistory()
		return a.foldFreed(before)
	}
	dropped := a.dropping(start, task)
	previous := ""
	if len(dropped) > 0 {
		if earlier, ok := recapOf(dropped[0]); ok {
			// the last fold's summary is updated, not summarised again as if it were a conversation
			previous, dropped = earlier, dropped[1:]
		}
	}
	summary, err := a.summariseWithModel(ctx, previous, a.transcriptOf(dropped))
	if err != nil {
		a.tell("the model could not summarise the older turns; shortening tool results instead")
		a.shrinkForCompaction()
		a.trimHistory()
		a.compactedAt = callBlocks(a.history) // the next try waits for new work, not for the next turn
		return a.foldFreed(before)
	}
	if len(summary) >= historyBytes(dropped) {
		// the recap would be as big as what it replaces: folding would cost a request and save nothing
		a.tell("the older turns are already smaller than a summary of them: nothing was folded")
		a.compactedAt = callBlocks(a.history) // asking again every turn would cost a request and the cache each time
		return a.foldFreed(before)
	}
	a.forget(a.dropping(start, task)...)
	a.noteFiles(dropped)
	kept := []llm.Message{{Role: "user", Content: FoldMark + "\n" + summary + a.fileBlocks() + a.planBlock()}}
	if task > 0 {
		// the job in hand is kept word for word: a summary is for what happened, and a model
		// working from a paraphrase of the request starts answering a question nobody asked
		kept = append(kept, a.history[task])
	}
	a.history = append(a.history[:1], append(kept, a.history[start:]...)...)
	a.compactedAt = callBlocks(a.history)
	a.fold = foldNote{summary: summary, dropped: len(dropped) + 1, before: before}
	a.work.made(roomFolded(len(dropped) + 1))
	a.emit(Event{Kind: Summary, Text: summary})
	return a.foldFreed(before)
}

// foldFreed reports what the fold saved, and republishes the conversation's cost so the footer and
// /ctx show the history as it now stands rather than as it was when the last reply arrived.
func (a *Agent) foldFreed(before int) int {
	a.publishUsage()
	// what the fold left standing is what a resume should read back, not what was folded away from it
	note := a.fold
	a.fold = foldNote{}
	a.reportSave(a.opts.Session.Folded(session.Fold{Kept: a.history[1:], Summary: note.summary,
		ReadFiles: a.readOnlyFiles(), ModifiedFiles: a.modifiedFiles, Dropped: note.dropped, TokensBefore: note.before}))
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

// compactCut picks where the kept part starts. The newest compactKeep tool calls are always kept; where
// the run knows how much room the recent work should have (KeepRecentTokens), the kept part reaches
// back until it holds that much, so a fold never throws away what the model was reading a moment ago.
// A cut never lands on a tool result: a call and its replies always move together. It also picks the
// newest user message in the part being folded away, which is kept for its wording rather than
// summarised. The second value is the index of that message, or 0 when the folded part holds none.
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

// transcriptOf flattens messages into the text the summariser is asked about: a call as the line that
// made it, and every result cut to what a summary can use, with the cut counted so the summariser
// knows there was more.
func (a *Agent) transcriptOf(messages []llm.Message) string {
	var b strings.Builder
	for _, m := range messages {
		switch {
		case m.Role == "tool":
			fmt.Fprintf(&b, "result %s %s:\n%s\n", m.CallID, m.Name, cutText(m.Content, compactLineCap))
		case len(m.ToolCalls) > 0:
			if text := strings.TrimSpace(m.Content); text != "" {
				fmt.Fprintf(&b, "assistant: %s\n", cutText(text, compactLineCap))
			}
			for _, call := range m.ToolCalls {
				fmt.Fprintf(&b, "call %s\n", callLine(call))
			}
		default:
			fmt.Fprintf(&b, "%s: %s\n", m.Role, cutText(m.Content, compactLineCap))
		}
	}
	if a.opts.CtxSize <= 0 {
		return b.String()
	}
	// the summary request must fit the window too: 40% of it, keeping the start and the latest work
	return tools.FormatResult(b.String(), int(float64(a.opts.CtxSize*2/5)*a.meter.bytesPerTok))
}

// cutText keeps the start of a text and says how much it left out.
func cutText(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return fmt.Sprintf("%s\n[… %d more characters]", s[:cut], len(s)-cut)
}

// callLine writes a call the way it would be typed: read_file(path="a.go"). A long argument, such as the
// text of a file being written, is cut, because what a summary needs is which file, not its contents.
func callLine(call llm.ToolCall) string {
	var args map[string]json.RawMessage
	if json.Unmarshal(call.Function.Arguments, &args) != nil {
		return call.Function.Name + "(" + cutText(string(call.Function.Arguments), compactArgCap) + ")"
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		var text string
		if json.Unmarshal(args[k], &text) != nil {
			text = string(args[k])
		}
		parts = append(parts, fmt.Sprintf("%s=%q", k, cutText(text, compactArgCap)))
	}
	return call.Function.Name + "(" + strings.Join(parts, ", ") + ")"
}

// keepSummary caps a runaway summary without flattening the sections it was written in: the model
// answers in short blocks, and a screen reads them better than one long line.
func keepSummary(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxSummaryChars {
		return text
	}
	return strings.TrimRight(string([]rune(text)[:maxSummaryChars]), " \n") + "…"
}

// summariseWithModel asks for the summary in its own request: no tools, a small answer, no history tailoring.
func (a *Agent) summariseWithModel(ctx context.Context, previous, transcript string) (string, error) {
	if transcript == "" {
		return "", fmt.Errorf("nothing to summarise")
	}
	var request strings.Builder
	if previous != "" {
		request.WriteString("<previous-summary>\n" + previous + "\n</previous-summary>\n\n")
	}
	// the transcript is quoted material, and the ask comes last: a model that sees calls and results
	// in its input goes on writing calls unless the final words tell it what the task is
	request.WriteString("<conversation>\n" + transcript + "\n</conversation>\n\n" + compactAsk)
	turn, err := a.client.ChatStream(ctx, llm.ChatRequest{
		Model: a.model,
		Messages: []llm.Message{
			{Role: "system", Content: compactPrompt},
			{Role: "user", Content: request.String()},
		},
		MaxTokens: compactMaxTokens,
		// a summary needs no thinking phase, and thinking costs the window we are trying to free
		ChatTemplateKwargs: map[string]any{"enable_thinking": false},
	}, nil)
	if err != nil {
		return "", err
	}
	summary := keepSummary(turn.Message.Content)
	if err := checkSummary(summary); err != nil {
		return "", err
	}
	return summary, nil
}

// checkSummary refuses a reply that is not a summary. A model shown a transcript of tool calls may go
// on writing them as text, and a recap made of that would replace real context with noise.
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
