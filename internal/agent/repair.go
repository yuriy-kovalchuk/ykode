package agent

import (
	"encoding/json"
	"fmt"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// repair patches servers that omit the role or id, or send arguments as an object instead of a string.
func repair(msg llm.Message) llm.Message {
	if msg.Role == "" {
		msg.Role = "assistant"
	}
	for i := range msg.ToolCalls {
		call := &msg.ToolCalls[i]
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%d", i+1)
		}
		if call.Type == "" {
			call.Type = "function"
		}
		if args, err := call.Function.ArgsJSON(); err == nil {
			call.Function.Arguments = args
		}
	}
	return msg
}

// storable returns a copy whose call arguments are whole JSON, which the session log's encoder needs
func storable(m llm.Message) llm.Message {
	if len(m.ToolCalls) == 0 {
		return m
	}
	calls := make([]llm.ToolCall, len(m.ToolCalls))
	copy(calls, m.ToolCalls)
	for i := range calls {
		if !json.Valid(calls[i].Function.Arguments) {
			calls[i].Function.Arguments = json.RawMessage("{}")
		}
	}
	m.ToolCalls = calls
	return m
}
