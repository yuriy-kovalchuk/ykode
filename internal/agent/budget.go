package agent

import (
	"encoding/json"
	"fmt"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

const (
	ctxSafetyTok   = 256 // slack for chat-template overhead
	minResponseTok = 64  // a response budget smaller than this is useless
	trimMarker     = "(older tool output trimmed to fit the server context)"
)

// request builds the chat body and gives the response the room the server's
// context window still has, so a long conversation degrades instead of being
// rejected by the server.
func (a *Agent) request() (llm.ChatRequest, error) {
	a.sanitizeHistory()
	req := llm.ChatRequest{
		Model:      a.model,
		Messages:   a.history,
		Tools:      a.specs(),
		ToolChoice: "auto",
		MaxTokens:  a.opts.MaxTokens,
	}
	if !a.opts.Think {
		// Verified against llama.cpp: "enable_thinking" is the key it honours;
		// "thinking_enabled" is silently ignored.
		req.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
	}
	if a.opts.CtxSize > 0 {
		used := promptTokens(a.history)
		room := a.opts.CtxSize - used - ctxSafetyTok
		if room < minResponseTok {
			return llm.ChatRequest{}, fmt.Errorf(
				"conversation is about %d tokens but the server context is %d; use /reset or raise --ctx", used, a.opts.CtxSize)
		}
		if room < req.MaxTokens {
			req.MaxTokens = room
		}
	}
	return req, nil
}

// sanitizeHistory makes every stored message safe to marshal. A tool call cut
// off by max_tokens holds arguments that are not valid JSON, and json.RawMessage
// goes straight to the encoder, which would fail the whole request.
func (a *Agent) sanitizeHistory() {
	for i := range a.history {
		for j := range a.history[i].ToolCalls {
			if args := a.history[i].ToolCalls[j].Function.Arguments; !json.Valid(args) {
				a.history[i].ToolCalls[j].Function.Arguments = json.RawMessage("{}")
			}
		}
	}
}

// promptTokens estimates a request's size. llama.cpp does not always expose
// /tokenize, and it measured about 3.3 bytes per token for this payload, so
// divide by 3 and let the margin absorb the difference.
func promptTokens(history []llm.Message) int {
	bytes := 0
	for _, m := range history {
		bytes += len(m.Content) + len(m.Name) + len(m.ToolCallID)
		for _, c := range m.ToolCalls {
			bytes += len(c.ID) + len(c.Function.Name) + len(c.Function.Arguments)
		}
	}
	return bytes/3 + 4*len(history)
}

// trimHistory replaces the contents of the oldest large tool results with a
// marker until the estimate fits. Structure is left alone: every tool_call
// still has its answer, and the system prompt and recent turns stay intact.
func (a *Agent) trimHistory() {
	if a.opts.CtxSize <= 0 {
		return
	}
	limit := a.opts.CtxSize * 3 / 4
	for i := 1; i < len(a.history) && promptTokens(a.history) > limit; i++ {
		if m := &a.history[i]; m.Role == "tool" && len(m.Content) > 400 {
			m.Content = trimMarker
			fmt.Fprintln(a.log, "  .. trimmed an old tool result to fit the context")
		}
	}
}
