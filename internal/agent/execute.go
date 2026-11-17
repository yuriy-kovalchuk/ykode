package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// runTool never returns an error: whatever went wrong becomes result text the model can act on. Its
// answer carries a picture too, when the tool read one.
func (a *Agent) runTool(ctx context.Context, call llm.ToolCall, id string) tools.Result {
	name := call.Function.Name
	args := a.normalizedArgs(name, call.Function.Arguments)
	a.emit(Event{Kind: ToolStart, Tool: name, Args: string(args)})
	if ctx.Err() != nil {
		return a.failedTool(name, string(args), "cancelled before this tool ran")
	}
	tool, ok := a.tools.Get(name)
	if !ok {
		return a.failedTool(name, string(args), fmt.Sprintf("unknown tool %q; available tools: %s", name, strings.Join(a.tools.Names(), ", ")))
	}
	// repair could not parse these arguments, so check them before any tool sees them
	if _, err := call.Function.ArgsJSON(); err != nil {
		return a.failedTool(name, string(args), err.Error())
	}
	targets, err := tool.Targets(args)
	if err != nil {
		return a.failedTool(name, string(args), err.Error())
	}
	outside := outsidePaths(targets)
	if !a.permit(tool, name, string(args), outside) {
		return a.failedTool(name, string(args), "the user refused this tool call")
	}
	if len(outside) > 0 {
		ctx = tools.WithApprovals(ctx, outside)
	}
	res, err := tools.CallOf(tool, ctx, args)
	if err != nil {
		return a.failedTool(name, string(args), err.Error())
	}
	res.Text = tools.FormatResult(res.Text, a.opts.MaxResult)
	a.blindToPictures(&res)
	a.emit(Event{Kind: ToolEnd, Tool: name, Args: string(args), Text: res.Text, Change: detailOf(tool).Change})
	return res
}

// blindToPictures tells the model it is not being shown a picture this turn, which the request leaves
// out because the model has no image reader for it. The bytes stay in the conversation, so a model
// switched to one that reads pictures is sent them; the words say why this one saw nothing.
func (a *Agent) blindToPictures(res *tools.Result) {
	if a.opts.Vision || len(res.Attachments) == 0 {
		return
	}
	res.Text += "\n[this model reads no pictures: the image was left out of that request]"
}

// failedTool turns a tool failure into the answer the model is given: text, and nothing beside it.
func (a *Agent) failedTool(name, args, why string) tools.Result {
	return tools.Result{Text: a.failTool(name, args, why)}
}

// failTool turns a tool failure into the text the model is told, after reporting it.
func (a *Agent) failTool(name, args, why string) string {
	a.emit(Event{Kind: ToolFail, Tool: name, Args: args, Text: why})
	return "error: " + why
}

// permit decides whether a call may run, asking the user when nothing already covers it. An
// outside-root path is asked about even under --yes and an "always" never covers it, so remembering
// one would only spread it over the same tool inside the project, where nobody was asked.
func (a *Agent) permit(tool tools.Tool, name, args string, outside []string) bool {
	needsAsk := tool.NeedsConfirm() && !a.opts.AssumeYes
	switch {
	case len(outside) == 0 && !needsAsk:
		return true
	case len(outside) == 0 && a.perms.allows(name, args):
		return true // an "always" from earlier in this run, or an entry from settings.yaml
	}
	verdict := a.emit(Event{Kind: Confirm, Tool: name, Args: args, Text: describe(name, args, outside), Target: outside})
	if verdict == Refuse {
		return false
	}
	if verdict == AllowAlways && len(outside) == 0 {
		a.perms.add(name, args)
	}
	return true
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
