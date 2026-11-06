package agent

import (
	"fmt"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// repair patches up what a server sent: some omit the role or the tool call id,
// and several send arguments as an object instead of the JSON string the OpenAI
// format specifies.
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

// toolCallsRunnable reports whether every call has complete arguments.
func toolCallsRunnable(calls []llm.ToolCall) bool {
	for _, c := range calls {
		if _, err := c.Function.ArgsJSON(); err != nil {
			return false
		}
	}
	return true
}
