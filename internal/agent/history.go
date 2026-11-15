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
// turn, the oldest messages go, whole, until it fits.
func (a *Agent) makeRoom() {
	if a.opts.CtxSize <= 0 {
		return
	}
	var gone []string
	messages := 0
	for a.estimateTokens() > a.ceiling() {
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
	a.reportSave(a.opts.Session.Rewritten(a.history[1:]))
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
		unit := a.history[i:end]
		a.history = append(a.history[:i:i], a.history[end:]...)
		a.meter.anchorTokens = 0
		return unit
	}
	return nil
}

// sendingBytes counts every byte a request carries; the system prompt already sits at the head of history.
func (a *Agent) sendingBytes() int {
	return a.wireSpecBytes() + historyBytes(a.history)
}

func historyBytes(history []llm.Message) int {
	bytes := 0
	for _, m := range history {
		bytes += len(m.Content) + len(m.Name) + len(m.ToolCallID) + len(m.WireReasoning)
		for _, c := range m.ToolCalls {
			bytes += len(c.ID) + len(c.Function.Name) + len(c.Function.Arguments)
		}
	}
	return bytes
}

// Conversation returns the turns the model is being sent, system prompt first: a run that came back
// to a saved conversation draws what it already knows. Read it between turns, not while one runs -
// the history belongs to the goroutine answering.
func (a *Agent) Conversation() []llm.Message {
	return append([]llm.Message(nil), a.history...)
}
