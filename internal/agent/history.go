package agent

import (
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// estimateTokens guesses what the server will count, using the ratio its last real count implied.
func (a *Agent) estimateTokens() int {
	rate := max(a.meter.bytesPerTok, minBytesPerTok)
	bytes := a.sendingBytes()
	// the server counted the conversation it just read; only what has moved since then needs guessing, so
	// the error of a byte ratio is not paid again over the whole conversation every turn
	if a.meter.anchorTokens > 0 && bytes >= a.meter.anchorBytes {
		return a.meter.anchorTokens + int(float64(bytes-a.meter.anchorBytes)/rate)
	}
	return int(float64(bytes)/rate) + 4*len(a.history)
}

// makeRoom is the last resort: when the conversation is over the ceiling even after a fold had its
// turn, the oldest messages go, whole, until a quarter of the window is free below the ceiling. Dropping
// only enough for this round moved the start of the prompt every round, and the server read the whole
// conversation again each time.
func (a *Agent) makeRoom() {
	if a.opts.CtxSize <= 0 || a.estimateTokens() <= a.ceiling() {
		return
	}
	target := a.ceiling() - a.opts.CtxSize/dropShare
	var gone []string
	messages := 0
	for a.estimateTokens() > target {
		unit := a.dropOldestUnit()
		if unit == nil {
			break
		}
		messages += len(unit)
		gone = append(gone, unitWords(unit))
	}
	if messages == 0 {
		return
	}
	a.work.made(roomDropped(messages))
	// naming what went is the difference between a model redoing a call on purpose and one that
	// merely finds a hole in its memory and guesses at it
	named := gone
	if len(named) > namedDrops {
		named = append(append([]string{}, named[:namedDrops]...), "...")
	}
	a.tell(fmt.Sprintf("dropped %s to fit the context: %s",
		counted(messages, "older message", "older messages"), strings.Join(named, "; ")))
}

// dropShare is the part of the window one drop frees below the ceiling: one in four.
const dropShare = 4

// namedDrops is how many of the dropped calls the notice names: enough for the model to see the kind
// of what went, short enough to read while a request runs.
const namedDrops = 3

// unitWords says what one dropped unit was, in the words the screen would have shown for the call.
func unitWords(unit []llm.Message) string {
	first := unit[0]
	if len(first.ToolCalls) > 0 {
		call := first.ToolCalls[0]
		said := call.Function.Name + " " + tools.Describe(call.Function.Name, string(call.Function.Arguments))
		if left := len(first.ToolCalls) - 1; left > 0 {
			said += fmt.Sprintf(" (+%d more calls)", left)
		}
		return said
	}
	if text := oneLine(first.Content, 80); text != "" {
		return first.Role + ": " + text
	}
	return first.Role
}

// dropOldestUnit removes the oldest message that may go: a call goes with its replies. What stays
// whatever happens: the system prompt, a fold's recap, the ask being answered, and the newest call
// with everything after it.
func (a *Agent) dropOldestUnit() []llm.Message {
	tail := len(a.history)
	for i := len(a.history) - 1; i > 0; i-- {
		if m := a.history[i]; m.Role == "user" || (m.Role == "assistant" && len(m.ToolCalls) > 0) {
			tail = i
			break
		}
	}
	for i := 1; i < tail; i++ {
		m := a.history[i]
		if _, recap := recapOf(m); recap || m.Role == "tool" || (m.Role == "user" && m.Content == a.task) {
			continue
		}
		end := i + 1
		for end < tail && a.history[end].Role == "tool" {
			end++
		}
		// the log's positions match only the history as it stands before the drop
		inStep := a.opts.Session.Count() == len(a.history)-1
		unit := a.history[i:end]
		a.history = append(a.history[:i:i], a.history[end:]...)
		gone := historyBytes(unit)
		a.histBytes -= gone
		a.histCount -= len(unit)
		a.meter.shrinkAnchor(gone) // the count the server gave still stands for what is left
		a.logDropped(i, end, inStep)
		return unit
	}
	return nil
}

// sendingBytes counts every byte a request carries; the system prompt already sits at the head of history.
// The total is kept as the conversation grows rather than walked, because the estimate runs several times
// a round-trip and once per message inside the trim loop. It is trusted only while the number of messages
// still matches: a path that rebuilds the conversation whole, or assigns to it directly, is caught by the
// count and measured again. A total quietly disagreeing with the history would drive the fold decision and
// the response cap off a number nothing measured, and that is worth one comparison to rule out.
func (a *Agent) sendingBytes() int {
	if a.histCount != len(a.history) {
		a.recountBytes()
	}
	return a.wireSpecBytes() + a.histBytes
}

// logDropped writes the one line that says history[from:to] went, rather than a copy of everything
// left standing: each copy carries every file the model read. The system prompt is never saved, so
// history[i] sits at position i-1 in the log - but only while the log was in step with the history
// before the drop. Otherwise the conversation is written whole, which cannot delete the wrong turns.
func (a *Agent) logDropped(from, to int, inStep bool) {
	if inStep && a.opts.Session.Dropped(from-1, to-1) == nil {
		return
	}
	a.reportSave(a.opts.Session.Rewritten(a.history[1:]))
}

// messageBytes is what one message costs a request: the fields that go on the wire, which is what the
// server counts.
func messageBytes(m llm.Message) int {
	bytes := len(m.Content) + len(m.Name) + len(m.ToolCallID) + len(m.Reasoning)
	for _, c := range m.ToolCalls {
		bytes += len(c.ID) + len(c.Function.Name) + len(c.Function.Arguments)
	}
	return bytes
}

// recountBytes brings the running total back to the conversation.
func (a *Agent) recountBytes() {
	a.histBytes, a.histCount = historyBytes(a.history), len(a.history)
}

func historyBytes(history []llm.Message) int {
	bytes := 0
	for _, m := range history {
		bytes += messageBytes(m)
	}
	return bytes
}

// Conversation returns the turns the model is being sent, system prompt first: a run that came back
// to a saved conversation draws what it already knows. The call list is copied rather than shared:
// the run repairs a cut-off call's arguments inside the stored message, and a reader holding the copy
// would otherwise watch one of its own strings change underneath it.
func (a *Agent) Conversation() []llm.Message {
	said := make([]llm.Message, len(a.history))
	copy(said, a.history)
	for i := range said {
		if len(a.history[i].ToolCalls) > 0 {
			said[i].ToolCalls = append([]llm.ToolCall(nil), a.history[i].ToolCalls...)
		}
	}
	return said
}
