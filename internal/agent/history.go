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
	// the server counted what it read, so only what moved since then needs guessing
	if a.meter.anchorTokens > 0 && bytes >= a.meter.anchorBytes {
		return a.meter.anchorTokens + int(float64(bytes-a.meter.anchorBytes)/rate)
	}
	return int(float64(bytes)/rate) + 4*len(a.history)
}

// makeRoom is the last resort: oldest messages go whole until a quarter of the window is free
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
	// naming what went is the difference between redoing a call and guessing at a hole in memory
	named := gone
	if len(named) > namedDrops {
		named = append(append([]string{}, named[:namedDrops]...), "...")
	}
	a.tell(fmt.Sprintf("dropped %s to fit the context: %s",
		counted(messages, "older message", "older messages"), strings.Join(named, "; ")))
}

// dropShare is the part of the window one drop frees below the ceiling: one in four.
const dropShare = 4

// namedDrops is how many dropped calls the notice names: enough to see the kind, short enough to read
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

// dropOldestUnit removes the oldest removable message, a call going with its replies
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

// sendingBytes counts what a request carries, kept as it grows and re-checked against the message count
func (a *Agent) sendingBytes() int {
	if a.histCount != len(a.history) {
		a.recountBytes()
	}
	return a.wireSpecBytes() + a.histBytes
}

// logDropped writes one line saying history[from:to] went, not a copy of everything standing
func (a *Agent) logDropped(from, to int, inStep bool) {
	if inStep && a.opts.Session.Dropped(from-1, to-1) == nil {
		return
	}
	a.reportSave(a.opts.Session.Rewritten(a.history[1:]))
}

// attachmentBytes is what an attachment costs the window, not the length of its base64
const attachmentBytes = 4000 // about 1200 tokens at the 3.3 bytes a token of prose measures

// messageBytes is what one message costs: the fields that go on the wire, which the server counts
func messageBytes(m llm.Message) int {
	bytes := len(m.Content) + len(m.Name) + len(m.ToolCallID) + len(m.Reasoning) + len(m.Attachments)*attachmentBytes
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

// Conversation returns the turns the model is sent, system prompt first, with the calls copied
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
