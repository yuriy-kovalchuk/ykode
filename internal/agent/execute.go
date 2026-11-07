package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/audit"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// runToolCalls appends one role:"tool" message per call: the API wants an answer for every tool_call.
func (a *Agent) runToolCalls(ctx context.Context, calls []llm.ToolCall) {
	for _, call := range calls {
		a.callSeq++
		id := fmt.Sprintf("r%d", a.callSeq)
		a.add(llm.Message{
			Role:       "tool",
			Content:    a.runTool(ctx, call, id),
			ToolCallID: call.ID,
			Name:       call.Function.Name,
			CallID:     id,
		})
	}
}

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
	// an "always" from earlier in this run, or an entry from settings.json, answers for us
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
		if verdict == AllowAlways {
			a.perms.add(name, string(args))
			decision = "allowlist"
		}
	}
	if len(outside) > 0 {
		ctx = tools.WithApprovals(ctx, outside)
	}
	a.record(id, name, argsText, targets, outside, decision, "started", "", tools.Detail{}, start)

	out, err := tool.Run(ctx, args)
	if err != nil {
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

// record appends one line to the audit log. A broken log never stops a run: it is reported once.
func (a *Agent) record(id, name, argsText string, targets []tools.Target, outside []string, decision, status, result string, detail tools.Detail, start time.Time) {
	if a.opts.Audit == nil {
		return
	}
	entry := audit.Entry{
		ID:          id,
		Turn:        a.turn,
		Root:        a.opts.Root,
		Model:       a.model,
		Tool:        name,
		Args:        argsText,
		Paths:       targetPaths(targets),
		OutsideRoot: outside,
		Decision:    decision,
		Status:      status,
		ExitCode:    detail.ExitCode,
		DurationMS:  time.Since(start).Milliseconds(),
		OutputPath:  detail.LogPath,
		Diff:        detail.Diff,
	}
	if result != "" {
		entry.ResultBytes = len(result)
		sum := sha256.Sum256([]byte(result))
		entry.ResultSHA = fmt.Sprintf("%x", sum[:8])
	}
	if err := a.opts.Audit.Append(entry); err != nil && !a.auditBroken {
		a.auditBroken = true // a log that will not write is reported once, not per call
		a.tell(err.Error())
	}
}

// targetPaths lists what a call touches, for the log.
func targetPaths(targets []tools.Target) []string {
	paths := make([]string, 0, len(targets))
	for _, t := range targets {
		paths = append(paths, t.Resolved)
	}
	return paths
}

// detailOf asks a tool what its last result says beyond the text.
func detailOf(tool tools.Tool) tools.Detail {
	if d, ok := tool.(tools.Detailed); ok {
		return d.LastDetail()
	}
	return tools.Detail{}
}
