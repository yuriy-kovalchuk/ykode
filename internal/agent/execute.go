package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// runTool never returns an error: whatever went wrong becomes result text the model can act on.
func (a *Agent) runTool(ctx context.Context, call llm.ToolCall, id string) string {
	name, args := call.Function.Name, call.Function.Arguments
	argsText := oneLine(string(args), 200)
	a.emit(Event{Kind: ToolStart, Tool: name, Args: argsText})
	start := time.Now()
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
		a.record(id, name, argsText, nil, nil, "auto", "error", "", tools.Detail{}, start)
		return a.failTool(name, err.Error())
	}
	outside := outsidePaths(targets)
	decision := "auto"
	if a.opts.AssumeYes && tool.NeedsConfirm() {
		decision = "yes_flag"
	}
	asked := (tool.NeedsConfirm() && !a.opts.AssumeYes) || len(outside) > 0
	// an "always" from earlier in this run, or an entry from settings.yaml, answers for us
	if asked && !(len(outside) > 0) && a.perms.allows(name, string(args)) {
		asked, decision = false, "allowlist"
	}
	// outside-root access is asked about even under --yes, and an "always" never covers it
	if asked {
		verdict := a.emit(Event{Kind: Confirm, Tool: name, Args: argsText, Text: describe(name, argsText, outside), Target: outside})
		if verdict == Refuse {
			a.record(id, name, argsText, targets, outside, "user_no", "refused", "", tools.Detail{}, start)
			return a.failTool(name, "the user refused this tool call")
		}
		decision = "user_yes"
		// an outside-root path is asked about every time, so remembering this "always" would only
		// spread it over the same tool inside the project, where nobody was asked
		if verdict == AllowAlways && len(outside) == 0 {
			a.perms.add(name, string(args))
			decision = "allowlist"
		}
	}
	if len(outside) > 0 {
		ctx = tools.WithApprovals(ctx, outside)
	}
	// a repeated search is answered from the first result instead of running again
	if guardedTools[name] {
		if earlier, times := a.repeats.claim(repeatKey(call), id, a.turn); earlier != nil {
			a.tell(fmt.Sprintf("the model asked for the same %s again (attempt %d)", name, times))
			reply := a.repeats.reply(name, *earlier, times)
			a.emit(Event{Kind: ToolEnd, Tool: name, Args: argsText, Text: oneLine(reply, 200)})
			return reply
		}
	}
	a.record(id, name, argsText, targets, outside, decision, "started", "", tools.Detail{}, start)

	out, err := tool.Run(ctx, args)
	if err != nil {
		if guardedTools[name] {
			a.repeats.release(repeatKey(call)) // it answered nothing, so trying again is not a repeat
		}
		if !readOnlyTools[name] {
			a.repeats.changed() // a command that failed may still have changed files on the way
		}
		a.record(id, name, argsText, targets, outside, decision, "error", "", tools.Detail{}, start)
		return a.failTool(name, err.Error())
	}
	result := tools.FormatResult(out, a.opts.MaxResult)
	status := "ok"
	switch {
	case ctx.Err() != nil:
		status = "cancelled"
	case detailOf(tool).TimedOut:
		status = "timeout"
	}
	a.record(id, name, argsText, targets, outside, decision, status, result, detailOf(tool), start)
	a.emit(Event{Kind: ToolEnd, Tool: name, Args: argsText, Text: oneLine(result, 200)})
	if !readOnlyTools[name] {
		a.repeats.changed() // the project changed: what was read before is no longer the answer
	}
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
