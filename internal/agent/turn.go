package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// Ask runs model round-trips until the model replies without a tool call; a cancelled context ends quietly.
func (a *Agent) Ask(ctx context.Context, prompt string) error {
	a.add(llm.Message{Role: "user", Content: prompt})
	a.task = prompt
	a.repeats.changed() // between prompts the user may have changed the files: nothing is a repeat yet

	usedTools, nudged, forceTool, emptyNudged, loops := false, false, false, false, 0
	for turn := 1; ; turn++ {
		a.turn = turn
		a.emit(Event{Kind: TurnStart, Turn: turn})
		if a.underPressure() {
			if freed := a.shrinkWhatAged(keepResults); freed > 0 {
				a.tell(fmt.Sprintf("shortened older tool output to fit the context (%s)", byteSize(freed)))
			}
		}
		// asked again after the shortening, on the same line: if it still does not fit, compaction earns its call
		if a.underPressure() && a.canCompact() && a.foldOverdue() {
			// shortening the results was not enough: pay one request to fold the older turns into a summary
			a.tell("the conversation is out of room: asking the model to summarise the older turns")
			a.Compact(ctx)
		}
		a.trimHistory() // last resort: blank what is left, then drop whole calls
		// what a front end reads next is what this request is built from, not what the last reply cost
		a.publishUsage()
		req, err := a.request(forceTool)
		forceTool = false
		if err != nil {
			return err
		}
		msg, finish, err := a.chat(ctx, req)
		if errors.Is(err, errRepeating) {
			// the half-written reply is dropped: it holds nothing but the loop
			a.emit(Event{Kind: TurnEnd, Turn: turn})
			if loops++; loops > 1 || turn >= a.opts.MaxTurns {
				a.tell("the reply went in circles again: asking for an answer with no tools")
				return a.forceAnswer(ctx, "No tools now. Answer in text from what you have already read, and say what you could not confirm.")
			}
			a.tell("the reply kept repeating the same lines: stopped it and told the model to move on")
			a.modelNote("(your last reply kept repeating the same lines and was stopped. Do not go over it again: make the next tool call, or answer from what you have.)")
			continue
		}
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
				a.modelNote("(your reply was cut off by max_tokens and the tool call was not valid JSON; make it smaller or split it into two calls)")
				if turn >= a.opts.MaxTurns {
					return a.outOfTurns(ctx)
				}
				continue
			}
		}
		if len(msg.ToolCalls) == 0 {
			if strings.TrimSpace(msg.Content) == "" {
				a.tell("the model returned no text")
				// an empty reply is not an answer: the task is left half done, so ask once to go on
				if !emptyNudged && turn < a.opts.MaxTurns {
					emptyNudged = true
					a.modelNote("(your reply was empty. Continue the task with the next tool call, or answer if it is done.)")
					continue
				}
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
		if a.repeats.isStuck() {
			a.tell("the model kept asking for the same call: stopping the tools and asking for an answer")
			return a.forceAnswer(ctx, "You keep asking for a result you already have. No tools now. Answer in text from what you have already read, and say what you could not confirm.")
		}
		if turn >= a.opts.MaxTurns {
			return a.outOfTurns(ctx)
		}
	}
}

func (a *Agent) chat(ctx context.Context, req llm.ChatRequest) (llm.Message, string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	thinking, answer := newLineRepeats(), newLineRepeats()
	looping := false
	turn, err := a.client.ChatStream(ctx, req, func(d llm.Delta) {
		if d.Reasoning != "" {
			a.emit(Event{Kind: Thinking, Text: d.Reasoning})
		}
		if d.Content != "" {
			a.emit(Event{Kind: Answer, Text: d.Content})
		}
		if d.Call != nil {
			a.emit(Event{Kind: ToolDraft, Tool: d.Call.Name, Text: d.Call.Args, Call: d.Call.Index})
		}
		if d.Timings != nil {
			a.emit(Event{Kind: Progress, Stats: Stats{PrefillTokSec: d.Timings.PromptPerSecond,
				DecodeTokSec: d.Timings.PredictedPerSecond, OutputTokens: d.Timings.PredictedN}})
		}
		if !looping && (thinking.add(d.Reasoning) || answer.add(d.Content)) {
			looping = true
			cancel() // the rest of this reply would be more of the same, for as long as the window lasts
		}
	})
	if looping {
		return llm.Message{}, "", errRepeating
	}
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
	if turn.Unfinished {
		// accepted, since some servers never send [DONE], but a dropped connection looks the same
		a.tell("the server ended the reply without saying it was finished; it may be cut short")
	}
	msg := repair(turn.Message)
	if a.opts.SendReasoning {
		msg.WireReasoning = msg.Reasoning
	}
	return msg, turn.FinishReason, nil
}
