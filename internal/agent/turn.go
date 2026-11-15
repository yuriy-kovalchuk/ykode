package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// Ask runs model round-trips until the model replies without a tool call; a cancelled context ends quietly.
// It is what marks the run busy: everything in the conversation belongs to this goroutine while it is
// in flight, and the methods that change it from outside refuse rather than trusting a caller to know.
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
	a.add(llm.Message{Role: "user", Content: asked})
	a.task = asked // what the history holds, which is what compaction looks for

	usedTools, textNudged, loops := false, false, 0
	// a reply that ends the turn without finishing the work is asked to go on, a bounded number of
	// times: the user should not have to type "continue" for a run that only lost its footing
	emptyNudges, cutNudges, pausedNudged, overflowed := 0, 0, false, false
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
			// the half-written reply is dropped: it holds nothing but the loop
			a.emit(Event{Kind: TurnEnd, Turn: turn})
			if loops++; loops > 1 || a.outOfRounds(turn) {
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
			if llm.IsOverflow(err) && !overflowed {
				// the server counted more tokens than the estimate did. A refusal that names the number is
				// the one exact measurement available here, so it is taken as the count rather than nudging a
				// guess: the estimate is what just failed. Where the server gave no number, the ratio is what
				// has to move, and it moves down because the conversation is bigger than the bytes said.
				overflowed = true
				count := llm.StatedPromptTokens(err)
				if count > 0 {
					a.tell(fmt.Sprintf("the server says the conversation is %d tokens and does not fit its window: making room and asking again", count))
					a.meter.toldByServer(count, a.sendingBytes())
				} else {
					a.tell("the server says the conversation does not fit its window: making room and asking again")
					a.meter.bytesPerTok = max(a.meter.bytesPerTok*3/4, minBytesPerTok)
					a.meter.anchorTokens = 0
				}
				if a.Compact(ctx) <= 0 {
					for i := 0; i < 8 && a.dropOldestUnit() != nil; i++ {
					}
					a.reportSave(a.opts.Session.Rewritten(a.history[1:]))
				}
				continue
			}
			return fmt.Errorf("chat turn %d: %w", turn, err)
		}
		wroteCall := len(msg.ToolCalls) == 0 && hasTextCall(msg.Content)
		if wroteCall {
			if calls := a.textCalls(msg.Content, turn); len(calls) > 0 && finish != finishLength {
				// the call is whole and names a tool we have: it runs, as the call the model meant
				msg.ToolCalls, wroteCall = calls, false
				if !textNudged {
					textNudged = true // said once per ask, however many calls arrive this way
					a.tell("the model wrote a tool call as text: running it as the call it meant")
				}
			}
			msg.Content = withoutTextCalls(msg.Content) // the answer never carries the markup
		}
		if finish == finishLength {
			// a call stopped mid-tag never closed, so everything after its opening was never answer.
			// It is not read as a call and the model is not told it wrote one: the next lines report the
			// reply running out of tokens, which is what actually happened.
			msg.Content = withoutCutCalls(msg.Content)
		}
		a.add(msg)

		if finish == finishLength {
			a.tell("the response hit max_tokens and may be incomplete")
			if !toolCallsRunnable(msg.ToolCalls) {
				// a cut-off tool call cannot run: tell the model rather than hand broken arguments over
				a.modelNote("(your reply was cut off by max_tokens and the tool call was not valid JSON; make it smaller or split it into two calls)")
				if a.outOfRounds(turn) {
					return a.outOfTurns(ctx)
				}
				continue
			}
		}
		if wroteCall && !textNudged && !a.outOfRounds(turn) {
			textNudged = true
			a.tell("the model wrote a tool call as text instead of calling it: asking once to call it properly")
			a.modelNote("(you wrote a tool call as plain text, so it did not run. Make the call through the tool interface, or answer in words if you need no tool.)")
			continue
		}
		if len(msg.ToolCalls) == 0 {
			said := strings.TrimSpace(msg.Content)
			switch {
			case finish == finishLength && cutNudges < maxResumeNudges && !a.outOfRounds(turn):
				// the reply ran into max_tokens: all of it thinking, or an answer stopped mid-sentence.
				// Either way the turn is not over, and ending it here leaves the user to type "continue"
				cutNudges++
				if said == "" {
					a.tell("the reply ran out of tokens while the model was still thinking: asking it to go on with less")
					a.modelNote("(your reply ran out of tokens while you were still thinking, before any answer or tool call. Think briefly this time: make the next tool call, or answer.)")
				} else {
					a.tell("the reply was cut off: asking the model to carry on from where it stopped")
					a.modelNote("(your reply was cut off by the token limit. Carry on from exactly where it stopped, without repeating what you already wrote.)")
				}
				continue
			case said == "":
				a.tell("the model returned no text")
				// an empty reply is not an answer: the task is left half done, so ask to go on
				if emptyNudges < maxResumeNudges && !a.outOfRounds(turn) {
					emptyNudges++
					a.modelNote("(your reply was empty. Continue the task with the next tool call, or answer if it is done.)")
					continue
				}
			case usedTools && !pausedNudged && !a.outOfRounds(turn) && a.stoppedHalfway(said):
				// it said what it would do next and then did not do it, or left steps of its own plan
				// open: a model that loses the thread mid-task stops like this, and one line brings it back
				pausedNudged = true
				a.tell("the model stopped with work still open: asking it once to carry on")
				a.modelNote("(you stopped before the work was finished. If you are waiting for the user, say what you need in one line. Otherwise carry on with the next tool call" + a.planReminderForPause() + ".)")
				continue
			}
			return nil
		}

		usedTools = true
		a.runToolCalls(ctx, msg.ToolCalls)
		if a.outOfRounds(turn) {
			return a.outOfTurns(ctx)
		}
	}
}

// maxResumeNudges is how many times one kind of unfinished reply is asked to go on within one ask.
const maxResumeNudges = 2

// announcesRe is a last line that says what comes next rather than what was found: "Let me update the
// tests now", "Next, I'll run the build". A reply that ends on one has not ended the work.
var announcesRe = regexp.MustCompile(`(?i)^(?:(?:now|next|then|ok|okay|alright|first|great|good)[,.!]?\s+)*(?:let me|let's|i'll|i will|i'm going to|i am going to|i need to|i'm now going to|now i|next i|i now)\b`)

// stoppedHalfway says a reply with no tool call reads as a pause rather than an answer: its last line
// announces the next step or ends in a colon, or the plan the model wrote still has steps open. A
// reply that ends by asking the user something is a real stop, whatever the plan says.
func (a *Agent) stoppedHalfway(said string) bool {
	last := said[strings.LastIndexByte(said, '\n')+1:]
	last = strings.TrimSpace(strings.Trim(strings.TrimSpace(last), "*_`"))
	if asksTheUser(said) || strings.HasPrefix(strings.ToLower(last), "let me know") {
		return false // it is waiting for the user, and says so
	}
	if strings.HasSuffix(last, ":") || announcesRe.MatchString(last) {
		return true
	}
	steps := a.opts.Plan.Steps()
	return len(steps) > 0 && !tools.Settled(steps)
}

// questionRe is a sentence that ends in a question mark, at the end of a line or before the next one.
var questionRe = regexp.MustCompile(`\?[*_` + "`" + `)]*(?:\s|$)`)

// asksTheUser says the reply's closing paragraph puts a question to the user. A report often ends
// "Want me to fix both? I'd keep the bump separate.": the question is not on the last line, and
// the reply is still waiting for an answer.
func asksTheUser(said string) bool {
	said = strings.TrimSpace(said)
	return questionRe.MatchString(said[strings.LastIndex(said, "\n\n")+1:])
}

// planReminderForPause adds the plan to the line that asks a paused model to go on, when it has one.
func (a *Agent) planReminderForPause() string {
	if steps := a.opts.Plan.Steps(); len(steps) == 0 || tools.Settled(steps) {
		return ""
	}
	return ", and update the plan as each step is finished"
}

// prepareTurn gets the conversation ready for the next round-trip: asks typed meanwhile join it, and
// what no longer fits the window is folded into a summary, then - if that was not enough - dropped.
func (a *Agent) prepareTurn(ctx context.Context, turn int) {
	// an ask typed while this turn ran joins the conversation here: the round-trip in flight and
	// the tools running are not interrupted
	for _, ask := range a.takeWaiting() {
		a.add(ask)
		a.task = ask.Content
		a.emit(Event{Kind: Steered, Turn: turn, Text: ask.Content})
	}
	// a fold reads the results at their full length, so it goes first: whatever it cannot fold, the
	// drop below sheds whole, and a message is never rewritten in place
	if a.underPressure() && a.canCompact() && a.foldOverdue() {
		a.tell("the conversation is out of room: asking the model to summarise the older turns")
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
	thinking, answer := newLineRepeats(), newLineRepeats()
	looping := false
	stopwatch := newPace(time.Now, a.meter.bytesPerTok)
	var shown callHider // a call written as text is not drawn as part of the answer
	// everything in the conversation up to here is about to be read by the model; if this request never
	// reaches it, what was added for it stays whole so the text is not lost on a call that was never made
	a.seenThrough = len(a.history)
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
		if d.Timings != nil {
			stopwatch.exact = true // the server reports its own speeds as it goes
			a.emit(Event{Kind: Progress, Stats: Stats{PrefillTokSec: d.Timings.PromptPerSecond,
				DecodeTokSec: d.Timings.PredictedPerSecond, OutputTokens: d.Timings.PredictedN}})
		}
		if decode, due := stopwatch.add(len(d.Reasoning) + len(d.Content) + callBytes(d.Call)); due {
			// a server that only reports speeds at the end: the decode speed on screen is measured here
			a.emit(Event{Kind: Progress, Stats: Stats{DecodeTokSec: decode, OutputTokens: int(stopwatch.total), Estimated: true}})
		}
		if !looping && (thinking.add(d.Reasoning) || answer.add(d.Content)) {
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
		// the server never said how fast: the speed measured here stands, and the count measured here
		// stands only where the server gave none - a number the server counted is not thrown away
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
		// A dropped connection and a server that never sends [DONE] look identical from here, so the
		// first one is worth saying and every one after it is noise: a server that ends all its replies
		// this way would otherwise warn once per round-trip for the whole session.
		a.tell("the server ended a reply without saying it was finished; it may be cut short, or this server may simply never say so")
	}
	msg := repair(turn.Message)
	if a.opts.SendReasoning {
		msg.WireReasoning = msg.Reasoning
	}
	return msg, turn.FinishReason, nil
}

// callBytes is the size of a piece of a tool call being written, which streams like text does.
func callBytes(c *llm.CallDelta) int {
	if c == nil {
		return 0
	}
	return len(c.Args)
}

// withEnvironment ends an ask with the date and the git state when they differ from what the last ask
// said: the model learns of a commit or a new day from the message that follows it, and a run that
// changed nothing adds nothing.
func (a *Agent) withEnvironment(ask string) string {
	note := environmentNote(a.opts.Root, time.Now())
	if note == a.environment {
		return ask
	}
	a.environment = note
	return ask + session.EnvironmentOpen + note + session.EnvironmentClose
}

// lastEnvironment is the note the newest ask in a conversation carried, so a resumed run repeats it only
// when something changed.
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
