package agent

import (
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

const trimMarker = "(older tool output trimmed to fit the server context)"

// estimateTokens guesses what the server will count, using the ratio its last real count implied.
func (a *Agent) estimateTokens() int {
	rate := max(a.meter.bytesPerTok, minBytesPerTok)
	return int(float64(a.sendingBytes())/rate) + 4*len(a.history)
}

// trimHistory keeps the conversation inside the window: large tool results are blanked first, then,
// if that was not enough, whole tool calls and their replies are dropped from the oldest end.
func (a *Agent) trimHistory() {
	if a.opts.CtxSize <= 0 || a.estimateTokens() <= a.opts.CtxSize*3/4 {
		return
	}
	a.trimResults(1, a.firstUnseen(), a.opts.CtxSize/2)
	// dropping whole calls is the emergency brake: it costs the model its recent work, so it waits
	// until the window is nearly full, after shortening and compaction have had their turn.
	for a.estimateTokens() > a.opts.CtxSize*9/10 && a.dropOldestToolCall() {
		a.tell("dropped an older tool call and its result to fit the context")
	}
	// a resumed conversation can be all prose, and prose has to go some day
	for a.estimateTokens() > a.opts.CtxSize*9/10 && a.dropOldestExchange() {
		a.tell("dropped an older turn to fit the context")
	}
	// last resort: a reply too big for what is left is cut rather than failing the request
	a.trimResults(a.firstUnseen(), len(a.history), a.opts.CtxSize*9/10)
}

// trimResults blanks large results in history[from:to], oldest first, until the estimate is under goal.
func (a *Agent) trimResults(from, to, goal int) {
	for i := from; i < to && a.estimateTokens() > goal; i++ {
		if m := &a.history[i]; m.Role == "tool" && len(m.Content) > 400 && !strings.HasPrefix(m.Content, shrinkMark) {
			a.forget(*m)
			m.Content = a.trimmed(m)
			a.tell("trimmed a tool result to fit the context")
		}
	}
}

// firstUnseen is where the replies the model has not read yet begin: every result is seen once in full.
func (a *Agent) firstUnseen() int {
	i := len(a.history)
	for i > 1 && a.history[i-1].Role == "tool" {
		i--
	}
	return i
}

// trimmed is what is left of a blanked result: a summary with a way back when it has an id, else a marker.
func (a *Agent) trimmed(m *llm.Message) string {
	if m.CallID == "" {
		return trimMarker
	}
	return a.summarize(m, m.CallID)
}

// dropOldestToolCall removes the oldest tool call with its replies, keeping the newest pair intact:
// past a certain point a summary of something the model already used is worth less than the room.
func (a *Agent) dropOldestToolCall() bool {
	var blocks []int
	for i, m := range a.history {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			blocks = append(blocks, i)
		}
	}
	if len(blocks) <= 1 {
		return false
	}
	i := blocks[0]
	end := i + 1
	for end < len(a.history) && a.history[end].Role == "tool" {
		end++
	}
	if end-i-1 < len(a.history[i].ToolCalls) {
		return false // a call whose replies are still owed must stay: the API pairs them
	}
	a.forget(a.history[i:end]...)
	a.history = append(a.history[:i], a.history[end:]...)
	return true
}

// dropOldestExchange removes the oldest question and the plain answers under it. Tool calls are
// dropped as whole blocks elsewhere; this is for the text in between, which nothing else sheds.
func (a *Agent) dropOldestExchange() bool {
	for i, m := range a.history {
		if i == 0 || m.Role != "user" {
			continue // the system prompt is not a turn
		}
		end := i + 1
		for end < len(a.history) && a.history[end].Role == "assistant" && len(a.history[end].ToolCalls) == 0 {
			end++
		}
		if !anotherTurnFollows(a.history[end:]) {
			return false // never drop the question being answered now
		}
		a.history = append(a.history[:i], a.history[end:]...)
		return true
	}
	return false
}

// anotherTurnFollows says a later user message is still there to answer.
func anotherTurnFollows(rest []llm.Message) bool {
	for _, m := range rest {
		if m.Role == "user" {
			return true
		}
	}
	return false
}

// sendingBytes counts every byte a request carries; the system prompt already sits at the head of history.
func (a *Agent) sendingBytes() int {
	bytes := 0
	for _, s := range a.specs() {
		bytes += len(s.Type) + len(s.Function.Name) + len(s.Function.Description) + len(s.Function.Parameters)
	}
	bytes += historyBytes(a.history)
	return bytes
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

// forget tells the tools that these results are no longer in front of the model, so their memory
// of having shown them must go with them: the next ask brings the text back, not a pointer to it.
func (a *Agent) forget(messages ...llm.Message) {
	if a.tools == nil {
		return
	}
	for _, m := range messages {
		if m.Role != "tool" {
			continue
		}
		if a.repeats != nil && m.CallID != "" {
			a.repeats.forgetID(m.CallID) // the same call again is now the only way back to that text
		}
		tool, ok := a.tools.Get(m.Name)
		if !ok {
			continue
		}
		if f, ok := tool.(tools.Forgetter); ok {
			f.Forget(tools.Shown{CallID: m.CallID, Text: m.Content})
		}
	}
}

// forgetAll clears every tool's memory, which is what a reset does to the conversation.
func (a *Agent) forgetAll() {
	if a.tools == nil {
		return
	}
	for _, tool := range a.tools.All() {
		if f, ok := tool.(tools.Forgetter); ok {
			f.ForgetAll()
		}
	}
}

// splitAll takes --all out of the query, wherever the user put it.
func splitAll(query string) (string, bool) {
	fields := strings.Fields(query)
	kept := fields[:0]
	all := false
	for _, f := range fields {
		if f == "--all" {
			all = true
			continue
		}
		kept = append(kept, f)
	}
	return strings.Join(kept, " "), all
}
