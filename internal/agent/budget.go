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
	if !a.opts.Think {
		// llama.cpp honours "enable_thinking" and silently ignores "thinking_enabled"
		req.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
	}
	if a.opts.CtxSize > 0 {
		used := a.estimateTokens()
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

// sanitizeHistory keeps history marshalable: a cut-off call holds invalid JSON that would fail the request.
func (a *Agent) sanitizeHistory() {
	for i := range a.history {
		for j := range a.history[i].ToolCalls {
			if args := a.history[i].ToolCalls[j].Function.Arguments; !json.Valid(args) {
				a.history[i].ToolCalls[j].Function.Arguments = json.RawMessage("{}")
			}
		}
	}
}

// estimateTokens guesses what the server will count, using the ratio its last real count implied.
func (a *Agent) estimateTokens() int {
	rate := max(a.meter.bytesPerTok, minBytesPerTok)
	return int(float64(a.sendingBytes())/rate) + 4*len(a.history)
}

// trimHistory marks the oldest large tool results until the estimate fits, keeping every tool_call paired.
func (a *Agent) trimHistory() {
	if a.opts.CtxSize <= 0 {
		return
	}
	limit := a.opts.CtxSize * 3 / 4
	for i := 1; i < len(a.history) && a.estimateTokens() > limit; i++ {
		if m := &a.history[i]; m.Role == "tool" && len(m.Content) > 400 {
			m.Content = trimMarker
			fmt.Fprintln(a.log, "  .. trimmed an old tool result to fit the context")
		}
	}
}
