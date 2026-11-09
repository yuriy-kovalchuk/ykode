package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// runTool never returns an error: whatever went wrong becomes result text the model can act on.
func (a *Agent) runTool(ctx context.Context, call llm.ToolCall, id string) string {
	name := call.Function.Name
	args := a.normalizedArgs(name, call.Function.Arguments)
	argsText := oneLine(string(args), 200) // for the audit log; events carry the whole call, a cut one is no longer JSON
	a.emit(Event{Kind: ToolStart, Tool: name, Args: string(args)})
	start := time.Now()
	if ctx.Err() != nil {
		return a.failTool(name, string(args), "cancelled before this tool ran")
	}
	tool, ok := a.tools.Get(name)
	if !ok {
		return a.failTool(name, string(args), fmt.Sprintf("unknown tool %q; available tools: %s", name, strings.Join(a.tools.Names(), ", ")))
	}
	// repair could not parse these arguments, so check them before any tool sees them
	if _, err := call.Function.ArgsJSON(); err != nil {
		return a.failTool(name, string(args), err.Error())
	}
	targets, err := tool.Targets(args)
	if err != nil {
		a.record(id, name, argsText, nil, nil, "auto", "error", "", tools.Detail{}, start)
		return a.failTool(name, string(args), err.Error())
	}
	outside := outsidePaths(targets)
	decision, allowed := a.permit(tool, name, string(args), outside)
	if !allowed {
		a.record(id, name, argsText, targets, outside, decision, "refused", "", tools.Detail{}, start)
		return a.failTool(name, string(args), "the user refused this tool call")
	}
	if len(outside) > 0 {
		ctx = tools.WithApprovals(ctx, outside)
	}
	// a repeated search is answered from the first result instead of running again
	if guardedTools[name] {
		if earlier, times := a.repeats.claim(repeatKey(call), id, a.turn); earlier != nil {
			a.tell(fmt.Sprintf("the model asked for the same %s again (attempt %d)", name, times))
			reply := a.repeats.reply(name, *earlier, times)
			a.emit(Event{Kind: ToolEnd, Tool: name, Args: string(args), Text: reply})
			return reply
		}
	}
	a.record(id, name, argsText, targets, outside, decision, "started", "", tools.Detail{}, start)

	out, err := tool.Run(ctx, args)
	if err != nil {
		if guardedTools[name] {
			a.repeats.release(repeatKey(call)) // it answered nothing, so trying again is not a repeat
		}
		if changesProject(name) {
			a.repeats.changed() // a command that failed may still have changed files on the way
		}
		a.record(id, name, argsText, targets, outside, decision, "error", "", tools.Detail{}, start)
		return a.failTool(name, string(args), err.Error())
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
	a.emit(Event{Kind: ToolEnd, Tool: name, Args: string(args), Text: result})
	if changesProject(name) {
		a.repeats.changed() // the project changed: what was read before is no longer the answer
	}
	return result
}

// failTool turns a tool failure into the text the model is told, after reporting it.
func (a *Agent) failTool(name, args, why string) string {
	a.emit(Event{Kind: ToolFail, Tool: name, Args: args, Text: why})
	return "error: " + why
}

// permit decides whether a call may run, asking the user when nothing already covers it, and names how
// it was decided for the audit log. An outside-root path is asked about even under --yes and an
// "always" never covers it, so remembering one would only spread it over the same tool inside the
// project, where nobody was asked.
func (a *Agent) permit(tool tools.Tool, name, args string, outside []string) (decision string, allowed bool) {
	needsAsk := tool.NeedsConfirm() && !a.opts.AssumeYes
	switch {
	case len(outside) == 0 && !needsAsk:
		if a.opts.AssumeYes && tool.NeedsConfirm() {
			return "yes_flag", true
		}
		return "auto", true
	case len(outside) == 0 && a.perms.allows(name, args):
		return "allowlist", true // an "always" from earlier in this run, or an entry from settings.yaml
	}
	verdict := a.emit(Event{Kind: Confirm, Tool: name, Args: args, Text: describe(name, args, outside), Target: outside})
	switch {
	case verdict == Refuse:
		return "user_no", false
	case verdict == AllowAlways && len(outside) == 0:
		a.perms.add(name, args)
		return "allowlist", true
	}
	return "user_yes", true
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

// redundantCD is a command's opening "cd DIR &&" or "cd DIR;", with DIR quoted or not.
var redundantCD = regexp.MustCompile(`^\s*cd\s+(?:"([^"]*)"|'([^']*)'|([^\s;&|]+))\s*(?:&&|;)\s*`)

// withoutRootCD takes a leading cd into the project root off a command. exec_command already starts
// there, so the cd changes nothing, and it costs: a command that chains can never match an allow entry,
// so every `cd /project && go test` asked again, and the row on screen opened with a path the reader
// already knows. A cd anywhere else is the command's own business and stays.
func withoutRootCD(command, root string) string {
	m := redundantCD.FindStringSubmatch(command)
	if m == nil {
		return command
	}
	dir := m[1] + m[2] + m[3]
	if dir != "." && dir != "./" && filepath.Clean(dir) != filepath.Clean(root) {
		return command
	}
	if rest := strings.TrimSpace(command[len(m[0]):]); rest != "" {
		return rest
	}
	return command
}

// normalizedArgs is a call's arguments with the redundant cd gone from an exec_command, and anything
// else untouched.
func (a *Agent) normalizedArgs(name string, args json.RawMessage) json.RawMessage {
	if name != "exec_command" || a.opts.Root == "" {
		return args
	}
	var in map[string]json.RawMessage
	if json.Unmarshal(args, &in) != nil {
		return args
	}
	var command string
	if json.Unmarshal(in["command"], &command) != nil {
		return args
	}
	stripped := withoutRootCD(command, a.opts.Root)
	if stripped == command {
		return args
	}
	in["command"], _ = json.Marshal(stripped)
	if out, err := json.Marshal(in); err == nil {
		return out
	}
	return args
}

// changesProject says a tool may have changed what is on disk, so what was read before is no longer the
// answer. A search is not one, and neither is the plan, which only the screen and the model read.
func changesProject(name string) bool { return !readOnlyTools[name] && name != "plan" }
