package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// runToolCalls appends one role:"tool" message per call: the API wants an answer for every tool_call.
func (a *Agent) runToolCalls(ctx context.Context, calls []llm.ToolCall) {
	ids := a.nextCallIDs(len(calls)) // numbered in call order whether the work overlaps or not
	if a.allReadOnly(calls) && len(calls) > 1 {
		a.runAtOnce(ctx, calls, ids)
		return
	}
	for i, call := range calls {
		result := a.runTool(ctx, call, ids[i])
		if i == len(calls)-1 {
			result += a.planReminder(calls)
		}
		a.add(a.toolMessage(call, result, ids[i]))
	}
}

// planStaleCalls is how many calls a plan with open steps may go without being touched before the
// model is reminded it has one.
const planStaleCalls = 6

// planReminder is a line added to the newest result when the model has worked for a while without
// touching its plan. A model writes the list, gets absorbed in the work, and marks every step done in
// one go at the end: the list on the screen then says nothing while it matters. The reminder rides on
// a result the model is about to read anyway, so it costs no round-trip and moves nothing already sent.
func (a *Agent) planReminder(calls []llm.ToolCall) string {
	for _, call := range calls {
		if call.Function.Name == "plan" {
			a.sincePlan = 0
			return ""
		}
	}
	steps := a.opts.Plan.Steps()
	if len(steps) == 0 || tools.Settled(steps) {
		a.sincePlan = 0
		return ""
	}
	if a.sincePlan += len(calls); a.sincePlan < planStaleCalls {
		return ""
	}
	a.sincePlan = 0
	return "\n\n[ykode: the plan has not been updated for a while. If a step is finished, call plan now with it marked done and the next one active.]"
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
	results[len(results)-1] += a.planReminder(calls)
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

// nextCallIDs takes the next n result ids, the same r1, r2... the summaries and the audit log share.
func (a *Agent) nextCallIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		a.callSeq++
		ids[i] = fmt.Sprintf("r%d", a.callSeq)
	}
	return ids
}
