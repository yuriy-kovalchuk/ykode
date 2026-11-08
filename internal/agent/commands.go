package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// Undoer puts the most recent file change back; tools.UndoStack is the real thing.
type Undoer interface {
	Undo(force bool) (string, error)
	Empty() bool
}

// historyLimit caps how many logged calls one /history shows.
const historyLimit = 20

// CommandSpec is one slash command as a front end needs it: what to type, what may follow it,
// what it does, and whether it waits for the turn in flight.
type CommandSpec struct {
	Name  string // /ctx
	Args  string // [what]; empty when the command takes nothing
	Note  string // how full the window is, and what the server is caching
	Waits bool   // the turn in flight owns what this changes, so it waits
}

// Commands lists the slash commands once, in the order they are worth reading: /help prints this
// and the screen completes from it, so neither can fall behind the other.
func Commands() []CommandSpec {
	return []CommandSpec{
		{Name: "/help", Note: "show this"},
		{Name: "/usage", Note: "what the conversation has cost the server"},
		{Name: "/compact", Note: "fold the older turns into a summary the model writes", Waits: true},
		{Name: "/history", Args: "[what]", Note: "what this run did: a tool name, failed, outside, an id like r12, or --all for every run of this root"},
		{Name: "/settings", Args: "[key] [value]", Note: "what ykode is set to, and change one knob"},
		{Name: "/sessions", Note: "the conversations saved for this project, and how to come back to one"},
		{Name: "/ctx", Note: "how full the window is, and what the server is caching"},
		{Name: "/tools", Note: "what the model may call, and what each description costs"},
		{Name: "/model", Args: "[id]", Note: "which model this run asks, or ask a different one", Waits: true},
		{Name: "/think", Args: "[level]", Note: "how deeply the model thinks: " + strings.Join(llm.ThinkingLevels(), ", "), Waits: true},
		{Name: "/undo", Args: "[force]", Note: "put the most recent file change back; force also when it was edited since", Waits: true},
		{Name: "/reset", Note: "forget the conversation", Waits: true},
		{Name: "/exit", Note: "quit (ctrl+c at the prompt too; mid-request it cancels the request)"},
	}
}

// helpText is /help: the command list laid out, notes in a column.
func helpText() string {
	width := 0
	for _, c := range Commands() {
		width = max(width, len(strings.TrimSpace(c.Name+" "+c.Args)))
	}
	rows := []string{"commands:"}
	for _, c := range Commands() {
		rows = append(rows, fmt.Sprintf("  %-*s %s", width, strings.TrimSpace(c.Name+" "+c.Args), c.Note))
	}
	return strings.Join(rows, "\n")
}

// UsageLine reports what the conversation has cost, in the server's own numbers.
func UsageLine(u Usage, ctxSize int) string {
	if u.Turns == 0 {
		return "usage: nothing asked yet"
	}
	room := fmt.Sprintf("~%d tokens", u.EstTok)
	if ctxSize > 0 {
		room = fmt.Sprintf("~%d of %d tokens", u.EstTok, ctxSize)
	}
	return fmt.Sprintf("usage: %d turns · last prompt %d (cached %d) · generated %d · all prompts %d · next %s · %.1f bytes/token",
		u.Turns, u.PromptTok, u.CachedTok, u.GenTok, u.TotalPrompt, room, u.BytesPerTok)
}

// CommandIsSafeDuringATurn says whether a command only reads: /help, /usage and /history can
// answer while a turn works; /reset, /compact and /undo would cut the ground from under it.
func CommandIsSafeDuringATurn(line string) bool {
	name := commandName(line)
	if name == "/exit" || name == "/quit" {
		return true // quitting cancels the turn rather than fighting it
	}
	for _, c := range Commands() {
		if c.Name == name {
			return !c.Waits
		}
	}
	return true // an unknown command changes nothing
}

// CompactReport is what /compact says when it is done.
func CompactReport(freed int) string {
	return fmt.Sprintf("compacted: the conversation is about %d tokens smaller\n", freed)
}

func commandName(line string) string {
	if fields := strings.Fields(line); len(fields) > 0 {
		return fields[0]
	}
	return line
}

// Command runs a slash command and returns the text a front end should show.
// quit says the session is over; err is the caller's to report, never a reason to stop.
func (a *Agent) Command(ctx context.Context, line string) (text string, quit bool, err error) {
	name, arg := commandName(line), strings.TrimSpace(strings.TrimPrefix(line, commandName(line)))
	switch name {
	case "/exit", "/quit":
		return "", true, nil
	case "/help":
		return helpText() + "\n", false, nil
	case "/reset":
		a.Reset()
		a.emit(Event{Kind: Cleared}) // the numbers a front end shows belong to the conversation that went
		return "history cleared\n", false, nil
	case "/usage":
		return UsageLine(a.Usage(), a.CtxSize()) + "\n", false, nil
	case "/compact":
		return CompactReport(a.Compact(ctx)), false, nil
	case "/history":
		return a.History(arg, historyLimit), false, nil
	case "/sessions":
		return a.Sessions(sessionsLimit), false, nil
	case "/ctx":
		return a.ContextLine() + "\n", false, nil
	case "/tools":
		return a.ToolsLine() + "\n", false, nil
	case "/model":
		if arg == "" {
			return fmt.Sprintf("model: %s\n", a.Model()), false, nil
		}
		a.SetModel(arg)
		return fmt.Sprintf("model: %s (the next request asks for it; a model the server has to load waits before the first token)\n", arg), false, nil
	case "/think":
		if arg == "" {
			return fmt.Sprintf("thinking: %s (levels: %s)\n", a.ThinkingLevel(), strings.Join(llm.ThinkingLevels(), ", ")), false, nil
		}
		level := strings.ToLower(arg)
		if level == "on" { // the old switch still means what it meant
			level = llm.DefaultThinkingLevel
		}
		if _, err := llm.ThinkingLevelValue(level); err != nil {
			return "", false, fmt.Errorf("/think %s: %w", arg, err)
		}
		a.SetThinkingLevel(level)
		if level == llm.ThinkingOff {
			return "thinking: off: the model answers straight away\n", false, nil
		}
		return fmt.Sprintf("thinking: %s (the next request asks the server for that depth)\n", level), false, nil
	case "/undo":
		if a.opts.Undo == nil || a.opts.Undo.Empty() {
			return "nothing to undo: ykode has not changed a file yet\n", false, nil
		}
		said, err := a.opts.Undo.Undo(arg == "force")
		if err != nil {
			return "", false, err
		}
		a.repeats.changed() // a file just went back: an earlier search may no longer be the answer
		return said + "\n", false, nil
	default:
		return fmt.Sprintf("unknown command %q; try /help, /reset or /exit\n", line), false, nil
	}
}
