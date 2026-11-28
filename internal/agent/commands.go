package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// CommandSpec is one slash command as a front end needs it: typing, arguments, meaning, and waiting
type CommandSpec struct {
	Name  string // /ctx
	Args  string // [what]; empty when the command takes nothing
	Note  string // how full the window is, and what the server is caching
	Waits bool   // the turn in flight owns what this changes, so it waits
	Also  string // another name the same command answers to; empty when there is none
}

// Spellings are the names this command answers to.
func (c CommandSpec) Spellings() []string {
	if c.Also == "" {
		return []string{c.Name}
	}
	return []string{c.Name, c.Also}
}

// Commands lists the slash commands once, so /help and the completion cannot fall behind each other
func Commands() []CommandSpec {
	return []CommandSpec{
		{Name: "/help", Note: "the commands, narrowed by typing; enter writes one in the prompt box"},
		{Name: "/usage", Note: "what the conversation has cost the server, read from a panel of its own"},
		{Name: "/compact", Note: "fold the older turns into a summary the model writes", Waits: true},
		{Name: "/settings", Args: "[list | key]", Note: "opens the settings screen, where a knob is changed: the project's .ykode/settings.yaml when it has one, the global file when it has not; list shows every knob, a key explains one"},
		{Name: "/create-local-setting", Note: "make .ykode/settings.yaml for this project; /settings then changes it instead of the global file", Waits: true},
		{Name: "/sessions", Note: "the conversations saved for this project; on the screen, pick one and this run continues it", Waits: true},
		{Name: "/plan", Note: "the steps of the task the model is working through, as it last wrote them"},
		{Name: "/ctx", Note: "how full the window is, and what the server is caching, read from a panel of its own"},
		{Name: "/tools", Note: "what the model may call, and what each description costs"},
		{Name: "/model", Args: "[id | number]", Note: "lists the models this server offers; an id or a number switches to it", Waits: true},
		{Name: "/think", Args: "[level]", Note: "how deeply the model thinks: " + strings.Join(llm.ThinkingLevels(), ", "), Waits: true},
		{Name: "/reset", Note: "forget the conversation; the screen asks first", Waits: true},
		{Name: "/exit", Also: "/quit", Note: "quit (ctrl+c at the prompt too; mid-request it cancels the request)"},
	}
}

// commandColumn is the left column: the command, what may follow, and any name it also answers to
func commandColumn(c CommandSpec) string {
	shown := strings.TrimSpace(c.Name + " " + c.Args)
	if c.Also != "" {
		shown += " (" + c.Also + ")"
	}
	return shown
}

// helpText is /help: the command list laid out, notes in a column.
func helpText() string {
	width := 0
	for _, c := range Commands() {
		width = max(width, len(commandColumn(c)))
	}
	rows := []string{"commands:"}
	for _, c := range Commands() {
		rows = append(rows, fmt.Sprintf("  %-*s %s", width, commandColumn(c), c.Note))
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

// CommandIsSafeDuringATurn says which commands only read, so they can answer mid-turn
func CommandIsSafeDuringATurn(line string) bool {
	name := commandName(line)
	for _, c := range Commands() {
		for _, spelled := range c.Spellings() {
			if spelled == name {
				return !c.Waits
			}
		}
	}
	return true // an unknown command changes nothing
}

// CompactReport is what /compact says when it is done.
func CompactReport(freed int) string {
	if freed <= 0 {
		// the fold found nothing worth folding: a saving of zero is not what happened
		return "compact: nothing was folded\n"
	}
	return fmt.Sprintf("compacted: the conversation is about %d tokens smaller\n", freed)
}

func commandName(line string) string {
	if fields := strings.Fields(line); len(fields) > 0 {
		return fields[0]
	}
	return line
}

// Command runs a slash command: the text to show, whether the session ended, and an error to report
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
	case "/sessions":
		return a.Sessions(sessionsLimit), false, nil
	case "/plan":
		return a.opts.Plan.Report() + "\n", false, nil
	case "/ctx":
		return a.ContextLine() + "\n", false, nil
	case "/tools":
		return a.ToolsLine() + "\n", false, nil
	case "/model":
		return a.modelCommand(ctx, arg), false, nil
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
	default:
		return fmt.Sprintf("unknown command %q; try /help, /reset or /exit\n", line), false, nil
	}
}
