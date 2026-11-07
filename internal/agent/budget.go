package agent

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

const (
	ctxSafetyTok   = 256  // the least to leave free when the caller named no margin
	minResponseTok = 64   // a response budget smaller than this is useless
	minAnswerTok   = 1024 // what the answer keeps after a thinking phase takes its share
	trimMarker     = "(older tool output trimmed to fit the server context)"
)

// request builds the chat body and leaves the response the room the context window still has.
func (a *Agent) request(forceTool bool) (llm.ChatRequest, error) {
	a.sanitizeHistory()
	req := llm.ChatRequest{
		Model:      a.model,
		Messages:   a.history,
		Tools:      a.specs(),
		ToolChoice: "auto",
		MaxTokens:  a.opts.MaxTokens,
	}
	if forceTool {
		// servers that ignore "required" just keep answering, so the guard costs one turn
		req.ToolChoice = "required"
	}
	req.Sampling = a.opts.Thinking
	if level := a.ThinkingLevel(); level == llm.ThinkingOff {
		req.Sampling = a.opts.NoThink
		// llama.cpp honours "enable_thinking" and silently ignores "thinking_enabled"
		req.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
	} else if budget, known := llm.ThinkingBudgetTokens(level); known {
		req.ThinkingBudget = budget
	}
	if a.opts.CtxSize > 0 {
		used := a.estimateTokens()
		room := a.opts.CtxSize - used - cmp.Or(a.opts.SafetyMargin, ctxSafetyTok)
		if room < minResponseTok {
			return llm.ChatRequest{}, fmt.Errorf(
				"conversation is about %d tokens but the server context is %d; use /reset or raise --ctx", used, a.opts.CtxSize)
		}
		// no cap asked for: the room the window has left is the cap, so a reply cannot run past the prompt
		if req.MaxTokens == 0 || room < req.MaxTokens {
			req.MaxTokens = room
		}
		// thinking is billed inside the reply, so it never takes more than the reply can spare
		if req.ThinkingBudget > 0 {
			req.ThinkingBudget = min(req.ThinkingBudget, req.MaxTokens-minAnswerTok)
			if req.ThinkingBudget <= 0 {
				// a budget of 0 is left off the wire, and no budget means no limit at all: a reply this
				// small still thinks, but half of it stays for the answer
				req.ThinkingBudget = max(req.MaxTokens/2, 1)
			}
		}
	}
	return req, nil
}

// sanitizeHistory keeps history something a server accepts: a cut-off call holds invalid JSON, and a
// call whose reply was never written (the user pressed Ctrl-C mid-tool) makes the whole request fail.
func (a *Agent) sanitizeHistory() {
	for i := range a.history {
		for j := range a.history[i].ToolCalls {
			if args := a.history[i].ToolCalls[j].Function.Arguments; !json.Valid(args) {
				a.history[i].ToolCalls[j].Function.Arguments = json.RawMessage("{}")
			}
		}
	}

	repaired := make([]llm.Message, 0, len(a.history))
	for i := 0; i < len(a.history); i++ {
		m := a.history[i]
		repaired = append(repaired, m)
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		answered := map[string]bool{}
		j := i + 1
		for ; j < len(a.history) && a.history[j].Role == "tool"; j++ {
			answered[a.history[j].ToolCallID] = true
			repaired = append(repaired, a.history[j])
		}
		for _, call := range m.ToolCalls {
			if !answered[call.ID] {
				repaired = append(repaired, llm.Message{
					Role:       "tool",
					ToolCallID: call.ID,
					Name:       call.Function.Name,
					Content:    "this call never ran: the request was interrupted before it finished",
				})
			}
		}
		i = j - 1 // the replies that were already there have been copied
	}
	a.history = repaired
}

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
