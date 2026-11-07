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

// Help lists the commands; the model does the work, so this stays short.
const Help = `commands:
  /help   show this
  /usage    what the conversation has cost the server
  /compact  fold the older turns into a summary the model writes
  /history [what]  what this run did: a tool name, failed, outside, an id like r12,
           or --all for every run of this root
  /ctx        how full the window is, and what the server is caching
  /tools      what the model may call, and what each description costs
  /model [id] which model this run asks, or ask a different one
  /think [level]  how deeply the model thinks: off, minimal, low, medium, high, xhigh, max
  /undo [force]  put the most recent file change back; force also when it was edited since
  /reset    forget the conversation
  /exit   quit (Ctrl-C at the prompt too; mid-request it cancels the request)`

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

// A turn uses the conversation and the files, so a command that changes them waits its turn.
var waitsForTheTurn = map[string]bool{
	"/reset": true, "/compact": true, "/undo": true,
	"/model": true, "/think": true, // the turn in flight is already shaped by the old setting
}

// CommandIsSafeDuringATurn says whether a command only reads: /help, /usage and /history can
// answer while a turn works; /reset, /compact and /undo would cut the ground from under it.
func CommandIsSafeDuringATurn(line string) bool {
	name := commandName(line)
	if name == "/exit" || name == "/quit" {
		return true // quitting cancels the turn rather than fighting it
	}
	return !waitsForTheTurn[name]
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
		return Help + "\n", false, nil
	case "/reset":
		a.Reset()
		return "history cleared\n", false, nil
	case "/usage":
		return UsageLine(a.Usage(), a.CtxSize()) + "\n", false, nil
	case "/compact":
		return CompactReport(a.Compact(ctx)), false, nil
	case "/history":
		return a.History(arg, historyLimit), false, nil
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
		return said + "\n", false, nil
	default:
		return fmt.Sprintf("unknown command %q; try /help, /reset or /exit\n", line), false, nil
	}
}
