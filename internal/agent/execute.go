package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// runToolCalls appends one role:"tool" message per call. The API expects an
// answer for every tool_call, so refused and cancelled calls still get one.
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

// runTool never returns an error: whatever went wrong becomes result text the
// model can react to.
func (a *Agent) runTool(ctx context.Context, call llm.ToolCall) string {
	name, args := call.Function.Name, call.Function.Arguments
	argsText := oneLine(string(args), 200)
	fmt.Fprintf(a.log, "  -> %s %s\n", name, argsText)
	if ctx.Err() != nil {
		return "(cancelled before this tool ran)"
	}
	tool, ok := a.tools.Get(name)
	if !ok {
		available := strings.Join(a.tools.Names(), ", ")
		fmt.Fprintf(a.log, "  !! %s: unknown tool; available: %s\n", name, available)
		return fmt.Sprintf("error: unknown tool %q; available tools: %s", name, available)
	}
	// Check the arguments before the tool sees them: repair could not parse
	// them, so they arrived cut off or malformed.
	if _, err := call.Function.ArgsJSON(); err != nil {
		fmt.Fprintf(a.log, "  !! %s: %v\n", name, err)
		return "error: " + err.Error()
	}
	targets, err := tool.Targets(args)
	if err != nil {
		fmt.Fprintf(a.log, "  !! %s: %v\n", name, err)
		return "error: " + err.Error()
	}
	outside := outsidePaths(targets)
	// Outside-root access is asked about even under --yes: that flag skips
	// routine confirmations, not the project boundary.
	if (tool.NeedsConfirm() && !a.opts.AssumeYes) || len(outside) > 0 {
		if !a.opts.Confirm(describe(name, argsText, outside)) {
			fmt.Fprintln(a.log, "  !! refused by the user")
			return "(the user refused this tool call)"
		}
	}
	if len(outside) > 0 {
		ctx = tools.WithApprovals(ctx, outside)
	}

	out, err := tool.Run(ctx, args)
	if err != nil {
		fmt.Fprintf(a.log, "  !! %s: %v\n", name, err)
		return "error: " + err.Error()
	}
	result := tools.FormatResult(out)
	fmt.Fprintf(a.log, "  <- %s %s\n", name, oneLine(result, 120))
	return result
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
