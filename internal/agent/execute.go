package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// runToolCalls appends one role:"tool" message per call: the API wants an answer for every tool_call.
func (a *Agent) runToolCalls(ctx context.Context, calls []llm.ToolCall) {
	for _, call := range calls {
		a.history = append(a.history, llm.Message{
			Role:       "tool",
			Content:    a.runTool(ctx, call),
			ToolCallID: call.ID,
			Name:       call.Function.Name,
		})
	}
}

// runTool never returns an error: whatever went wrong becomes result text the model can act on.
func (a *Agent) runTool(ctx context.Context, call llm.ToolCall) string {
	name, args := call.Function.Name, call.Function.Arguments
	argsText := oneLine(string(args), 200)
	a.emit(Event{Kind: ToolStart, Tool: name, Args: argsText})
	if ctx.Err() != nil {
		return a.failTool(name, "cancelled before this tool ran")
	}
	tool, ok := a.tools.Get(name)
	if !ok {
		return a.failTool(name, fmt.Sprintf("unknown tool %q; available tools: %s", name, strings.Join(a.tools.Names(), ", ")))
	}
	// repair could not parse these arguments, so check them before any tool sees them
	if _, err := call.Function.ArgsJSON(); err != nil {
		return a.failTool(name, err.Error())
	}
	targets, err := tool.Targets(args)
	if err != nil {
		return a.failTool(name, err.Error())
	}
	outside := outsidePaths(targets)
	// outside-root access is asked about even under --yes, which only skips routine confirmations
	if (tool.NeedsConfirm() && !a.opts.AssumeYes) || len(outside) > 0 {
		decision := a.emit(Event{Kind: Confirm, Tool: name, Args: argsText, Text: describe(name, argsText, outside), Target: outside})
		if decision == Refuse {
			return a.failTool(name, "the user refused this tool call")
		}
	}
	if len(outside) > 0 {
		ctx = tools.WithApprovals(ctx, outside)
	}

	out, err := tool.Run(ctx, args)
	if err != nil {
		return a.failTool(name, err.Error())
	}
	result := tools.FormatResult(out, a.opts.MaxResult)
	a.emit(Event{Kind: ToolEnd, Tool: name, Args: argsText, Text: oneLine(result, 200)})
	return result
}

// failTool turns a tool failure into the text the model is told, after reporting it.
func (a *Agent) failTool(name, why string) string {
	a.emit(Event{Kind: ToolFail, Tool: name, Text: why})
	return "error: " + why
}

func outsidePaths(targets []tools.Target) []string {
	var outside []string
	for _, t := range targets {
		if t.Outside {
			outside = append(outside, t.Resolved)
		}
	}
	return outside
}

func describe(name, argsText string, outside []string) string {
	if len(outside) == 0 {
		return name + " " + argsText
	}
	return fmt.Sprintf("%s OUTSIDE the project root: %s\n  %s", name, strings.Join(outside, ", "), oneLine(argsText, 120))
}

// oneLine squashes s onto one short line for logging.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "..."
	}
	return s
}
