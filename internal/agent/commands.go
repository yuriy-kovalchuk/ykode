package agent

import (
	"context"
	"fmt"
	"strings"
)

// Undoer puts the most recent file change back; tools.UndoStack is the real thing.
type Undoer interface {
	Undo() (string, error)
	Empty() bool
}

// historyLimit caps how many logged calls one /history shows.
const historyLimit = 20

// Help lists the commands; the model does the work, so this stays short.
const Help = `commands:
  /help   show this
  /usage    what the conversation has cost the server
  /compact  fold the older turns into a summary the model writes
  /history [what]  what this run did: a tool name, "failed" or "outside"
  /undo     put the most recent file change back
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

// Command runs a slash command and returns the text a front end should show.
// quit says the session is over; err is the caller's to report, never a reason to stop.
func (a *Agent) Command(line string) (text string, quit bool, err error) {
	name, arg := strings.Fields(line)[0], strings.TrimSpace(strings.TrimPrefix(line, strings.Fields(line)[0]))
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
		freed := a.Compact(context.Background())
		return fmt.Sprintf("older tool results shortened; the conversation is about %d tokens smaller\n", freed), false, nil
	case "/history":
		return a.History(arg, historyLimit), false, nil
	case "/undo":
		if a.opts.Undo == nil || a.opts.Undo.Empty() {
			return "nothing to undo: ykode has not changed a file yet\n", false, nil
		}
		said, err := a.opts.Undo.Undo()
		if err != nil {
			return "", false, err
		}
		return said + "\n", false, nil
	default:
		return fmt.Sprintf("unknown command %q; try /help, /reset or /exit\n", line), false, nil
	}
}
