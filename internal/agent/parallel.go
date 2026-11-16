package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// runToolCalls appends one role:"tool" message per call: the API wants an answer for every tool_call.
func (a *Agent) runToolCalls(ctx context.Context, calls []llm.ToolCall) {
	ids := a.nextCallIDs(len(calls)) // numbered in call order whether the work overlaps or not
	if a.allReadOnly(calls) && len(calls) > 1 {
		a.runAtOnce(ctx, calls, ids)
		return
	}
	for i, call := range calls {
		a.add(a.toolMessage(call, a.runTool(ctx, call, ids[i]), ids[i]))
	}
}

// runAtOnce runs a batch of read-only calls together and keeps their replies in call order.
func (a *Agent) runAtOnce(ctx context.Context, calls []llm.ToolCall, ids []string) {
	results := make([]string, len(calls))
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(i int, call llm.ToolCall) {
			defer wg.Done()
			results[i] = a.runTool(ctx, call, ids[i])
		}(i, call)
	}
	wg.Wait()
	for i, call := range calls {
		a.add(a.toolMessage(call, results[i], ids[i]))
	}
}

func (a *Agent) toolMessage(call llm.ToolCall, content, id string) llm.Message {
	return llm.Message{Role: "tool", Content: content, ToolCallID: call.ID, Name: call.Function.Name, CallID: id}
}

// allReadOnly says every call in the batch is a tool that changes nothing.
func (a *Agent) allReadOnly(calls []llm.ToolCall) bool {
	for _, call := range calls {
		if !a.runsTogether(call.Function.Name) {
			return false
		}
	}
	return true
}

// nextCallIDs takes the next n result ids, the same r1, r2... the summaries and the saved conversation use.
func (a *Agent) nextCallIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		a.callSeq++
		ids[i] = fmt.Sprintf("r%d", a.callSeq)
	}
	return ids
}
