package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
)

// Ask runs round-trips until the model replies without a call, and marks the run busy while it does
func (a *Agent) Ask(ctx context.Context, prompt string) error {
	if !a.turnRunning.CompareAndSwap(false, true) {
		return fmt.Errorf("a turn is already running: it owns the conversation")
	}
	defer a.turnRunning.Store(false)
	return a.ask(ctx, prompt)
}

func (a *Agent) ask(ctx context.Context, prompt string) error {
	a.opts.Plan.DropIfSettled() // the steps belonged to the prompt that finished them
	a.work.started()            // what the last ask had to do says nothing about this one
	asked := a.withEnvironment(prompt)
	a.add(llm.Message{Role: "user", Content: asked, Attachments: a.picturesIn(prompt)})
	a.task = asked // what the history holds, which is what compaction looks for

	// stalls counts thinking-only replies in a row; one that is kept makes the next stall a fresh one
	textRan, overflowed, stalls := false, false, 0
	for turn := 1; ; turn++ {
		a.turn.Store(int64(turn))
		a.emit(Event{Kind: TurnStart, Turn: turn})
		a.prepareTurn(ctx, turn)
		req, err := a.request()
		if err != nil {
			return err
		}
		msg, finish, err := a.chat(ctx, req)
		if errors.Is(err, errRepeating) {
			a.emit(Event{Kind: TurnEnd, Turn: turn})
			a.tell("the reply kept repeating the same lines: stopped it")
			a.note("(the reply was stopped: it kept repeating the same lines)")
			return nil
		}
		if err != nil {
			// Close any half-streamed thinking block before reporting.
			a.emit(Event{Kind: TurnEnd, Turn: turn})
			if ctx.Err() != nil {
				a.note("(the user cancelled this request)")
				return nil
			}
			if llm.IsOverflow(err) && !overflowed {
				overflowed = true
				a.recoverFromOverflow(ctx, err)
				continue
			}
			return fmt.Errorf("chat turn %d: %w", turn, err)
		}
		cut := finish == finishLength
		if len(msg.ToolCalls) == 0 && hasTextCall(msg.Content) {
			if calls := a.textCalls(msg.Content, turn); len(calls) > 0 && !cut {
				msg.ToolCalls = calls // whole, and naming a tool we have: it runs as the call it meant
				if !textRan {
					textRan = true
					a.tell("the model wrote a tool call as text: running it as the call it meant")
				}
			}
			msg.Content = withoutTextCalls(msg.Content) // the answer never carries the markup
		}
		if cut {
			// a call stopped mid-tag never closed, so everything after its opening was never answer
			msg.Content = withoutCutCalls(msg.Content)
			a.tell("the response hit max_tokens and may be incomplete")
		}
		if stoppedAfterThinking(msg, cut) {
			// kept, it would teach the model that a turn may end at its thinking: the same request goes again
			if stalls < thinkingRetries && !a.outOfRounds(turn) {
				stalls++
				a.tell(fmt.Sprintf("the model stopped after thinking, before any call or answer: asking again (%d of %d)",
					stalls, thinkingRetries))
				continue
			}
			a.tell("the model returned no text")
			a.note("(the model stopped after its thinking, with no call and no answer)")
			return nil
		}
		stalls = 0
		a.add(msg)

		if len(msg.ToolCalls) == 0 {
			if strings.TrimSpace(msg.Content) == "" {
				a.tell("the model returned no text")
			}
			return nil
		}
		if cut {
			a.failCutCalls(msg.ToolCalls)
		} else {
			a.runToolCalls(ctx, msg.ToolCalls)
		}
		if a.outOfRounds(turn) {
			return fmt.Errorf("reached --max-turns (%d) before the model answered; send any message to carry on", a.opts.MaxTurns)
		}
	}
}

// recoverFromOverflow makes room after a refusal, taking the size the server named over the guess
func (a *Agent) recoverFromOverflow(ctx context.Context, err error) {
	if count := llm.StatedPromptTokens(err); count > 0 {
		a.tell(fmt.Sprintf("the server says the conversation is %d tokens and does not fit its window: making room and asking again", count))
		a.meter.toldByServer(count, a.sendingBytes())
	} else {
		a.tell("the server says the conversation does not fit its window: making room and asking again")
		a.meter.bytesPerTok = max(a.meter.bytesPerTok*3/4, minBytesPerTok)
		a.meter.anchorTokens = 0
	}
	if a.Compact(ctx) > 0 {
		return
	}
	// each drop already writes itself to the session log
	for i := 0; i < overflowDrops && a.dropOldestUnit() != nil; i++ {
	}
}

// thinkingRetries is how often one stall is sent again before the ask ends; each costs a round of thinking
const thinkingRetries = 2

// stoppedAfterThinking is a whole reply that thought its way to a step and then ended without taking it
func stoppedAfterThinking(msg llm.Message, cut bool) bool {
	return !cut && len(msg.ToolCalls) == 0 && strings.TrimSpace(msg.Content) == "" &&
		strings.TrimSpace(msg.Reasoning) != ""
}

// overflowDrops is how many of the oldest calls go when a refused request could not be folded.
const overflowDrops = 8

// failCutCalls answers every call of a reply cut at max_tokens without running any of them
func (a *Agent) failCutCalls(calls []llm.ToolCall) {
	ids := a.nextCallIDs(len(calls))
	for i, call := range calls {
		why := a.failedTool(call.Function.Name, string(call.Function.Arguments),
			"your reply was cut off by max_tokens, so this call did not run; make it smaller or split it")
		a.add(a.toolMessage(call, why, ids[i]))
	}
}

// prepareTurn readies the next round-trip: asks typed meanwhile join, and what does not fit goes
func (a *Agent) prepareTurn(ctx context.Context, turn int) {
	// an ask typed while this turn ran joins here, without interrupting the flight or the tools
	for _, ask := range a.takeWaiting() {
		a.add(ask)
		a.task = ask.Content
		a.emit(Event{Kind: Steered, Turn: turn, Text: ask.Content})
	}
	// a fold reads results at their full length so it goes first; what it cannot fold is shed whole
	if a.underPressure() && a.canCompact() && a.foldOverdue() {
		a.tell(fmt.Sprintf("the conversation is out of room, about %d of %d tokens with %d held back for the answer: "+
			"asking the model to summarise the older turns", a.estimateTokens(), a.opts.CtxSize, a.reserve()))
		a.work.made(roomFolding) // the clock is about to wait on a whole extra request: say what it waits on
		a.Compact(ctx)
	}
	a.makeRoom()
	// what a front end reads next is what this request is built from, not what the last reply cost
	a.publishUsage()
}

func (a *Agent) chat(ctx context.Context, req llm.ChatRequest) (llm.Message, string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	answer, looping := newLineRepeats(), false
	stopwatch := newPace(time.Now, a.meter.bytesPerTok)
	var shown callHider // a call written as text is not drawn as part of the answer
	turn, err := a.client.ChatStream(ctx, req, func(d llm.Delta) {
		if d.Reasoning != "" {
			a.emit(Event{Kind: Thinking, Text: d.Reasoning})
		}
		if d.Content != "" {
			if text := shown.feed(d.Content); text != "" {
				a.emit(Event{Kind: Answer, Text: text})
			}
		}
		if d.Call != nil {
			a.emit(Event{Kind: ToolDraft, Tool: d.Call.Name, Text: d.Call.Args, Call: d.Call.Index})
		}
		if p := d.Progress; p != nil && p.Total > 0 {
			a.emit(Event{Kind: Progress, Stats: Stats{ReadTokens: p.Processed, ReadTotal: p.Total}})
		}
		if d.Timings != nil {
			stopwatch.exact = true // the server reports its own speeds as it goes
			a.emit(Event{Kind: Progress, Stats: Stats{PrefillTokSec: d.Timings.PromptPerSecond,
				DecodeTokSec: d.Timings.PredictedPerSecond, OutputTokens: d.Timings.PredictedN}})
		}
		if decode, due := stopwatch.add(len(d.Reasoning) + len(d.Content) + callBytes(d.Call)); due {
			// a server that only reports speeds at the end: the decode speed on screen is measured here
			a.emit(Event{Kind: Progress, Stats: Stats{DecodeTokSec: decode, OutputTokens: int(stopwatch.total), Estimated: true}})
		}
		// thinking is not watched: its budget already ends it, and drafting code in it repeats lines honestly
		if !looping && answer.add(d.Content) {
			looping = true
			cancel() // the rest of this reply would be more of the same, for as long as the window lasts
		}
	})
	if text := shown.rest(); text != "" {
		a.emit(Event{Kind: Answer, Text: text})
	}
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
	if stats.DecodeTokSec == 0 && !stopwatch.exact {
		// where the server gave no speed or count, the ones measured here stand in their place
		tokens, perSecond := stopwatch.average()
		stats.DecodeTokSec, stats.Estimated = perSecond, perSecond > 0
		if stats.OutputTokens == 0 {
			stats.OutputTokens = tokens
		}
	}
	a.meter.observe(stats, a.sendingBytes())
	if r := a.meter.bytesPerTok; r != defaultBytesPerTok && (r < a.savedRatio*0.98 || r > a.savedRatio*1.02) {
		a.savedRatio = r
		a.reportSave(a.opts.Session.Learned(r)) // a run that comes back measures as this one does
	}
	a.publishUsage()
	a.emit(Event{Kind: TurnEnd, Turn: a.turnNo(), Stats: stats})
	if turn.Unfinished && !a.unfinishedNoted.Swap(true) {
		// a dropped connection and a server without [DONE] look the same, so only the first is said
		a.tell("the server ended a reply without saying it was finished; it may be cut short, or this server may simply never say so")
	}
	return repair(turn.Message), turn.FinishReason, nil
}

// callBytes is the size of a piece of a tool call being written, which streams like text does.
func callBytes(c *llm.CallDelta) int {
	if c == nil {
		return 0
	}
	return len(c.Args)
}

// withEnvironment ends an ask with date and git state only when they changed since the last ask
func (a *Agent) withEnvironment(ask string) string {
	note := environmentNote(a.opts.Root, time.Now())
	if note == a.environment {
		return ask
	}
	a.environment = note
	return ask + session.EnvironmentOpen + note + session.EnvironmentClose
}

// lastEnvironment is the note the newest ask carried, so a resume repeats it only on change
func lastEnvironment(history []llm.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != "user" {
			continue
		}
		if _, note := session.SplitEnvironment(history[i].Content); note != "" {
			return note
		}
	}
	return ""
}
