package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// View is one consumer of the agent's events: thinking and tool progress to the log, answers to stdout
type View struct {
	mu       sync.Mutex // tools may report from several goroutines
	out      io.Writer
	log      io.Writer
	colored  bool
	thinking bool
	answered bool
	// failed keeps the first write that did not land, so a piped run that lost its answer does not exit 0
	failed error
}

func NewView(out, log io.Writer) *View {
	return &View{out: out, log: log, colored: isTerminal(log)}
}

// Err is the first write that failed, nil when everything reached its stream
func (v *View) Err() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.failed
}

// say writes to the log, keeping the first failure.
func (v *View) say(format string, args ...any) {
	if _, err := fmt.Fprintf(v.log, format, args...); err != nil && v.failed == nil {
		v.failed = err
	}
}

// tell writes the answer, keeping the first failure.
func (v *View) tell(text string) {
	if _, err := io.WriteString(v.out, text); err != nil && v.failed == nil {
		v.failed = err
	}
}

// Emit renders one event; only a Confirm event cares about what comes back.
func (v *View) Emit(ev agent.Event) agent.Decision {
	v.mu.Lock()
	defer v.mu.Unlock()

	switch ev.Kind {
	case agent.Thinking:
		if !v.thinking {
			v.say("%s", v.dim()+"thinking: ")
			v.thinking = true
		}
		v.say("%s", ev.Text)
	case agent.Answer:
		v.endThinking()
		v.tell(ev.Text)
		v.answered = true
	case agent.TurnEnd:
		v.endThinking()
		if v.answered {
			v.tell("\n")
			v.answered = false
		}
		if ev.Stats.PromptTokens == 0 && ev.Stats.OutputTokens == 0 {
			return agent.Allow // server gave no timings
		}
		v.say("  .. in %d (cached %d) / out %d · prefill %.0f tok/s · decode %.0f tok/s · %s\n",
			ev.Stats.PromptTokens, ev.Stats.CachedTokens, ev.Stats.OutputTokens,
			ev.Stats.PrefillTokSec, ev.Stats.DecodeTokSec, ev.Stats.FinishReason)
	case agent.ToolStart:
		v.endThinking()
		v.say("  -> %s %s\n", ev.Tool, flat(ev.Args))
	case agent.ToolEnd:
		v.say("  <- %s %s\n", ev.Tool, flat(ev.Text))
	case agent.ToolFail:
		v.say("  !! %s: %s\n", ev.Tool, ev.Text)
	case agent.Summary:
		v.endThinking()
		v.say("  .. the older turns, folded into:\n")
		for _, line := range strings.Split(ev.Text, "\n") {
			v.say("     %s\n", line)
		}
	case agent.Notice:
		v.say("  .. %s\n", ev.Text)
	case agent.Confirm:
		v.say("  !! %s needs a yes: run with --yes\n", ev.Tool)
		return agent.Refuse
	}
	return agent.Allow
}

func (v *View) endThinking() {
	if v.thinking {
		v.say("%s", v.reset()+"\n")
		v.thinking = false
	}
}

func (v *View) dim() string {
	if v.colored {
		return "\x1b[2;3m" // dim + italic
	}
	return ""
}

func (v *View) reset() string {
	if v.colored {
		return "\x1b[0m"
	}
	return ""
}

// IsTerminal reports whether a stream is an interactive one, so a script can know nobody will answer.
func IsTerminal(f *os.File) bool { return isTerminal(f) }

// nullInfo is the null device's stat: writing reaches nobody, so it is never interactive
var nullInfo = func() os.FileInfo {
	info, err := os.Stat(os.DevNull)
	if err != nil {
		return nil
	}
	return info
}()

func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if nullInfo != nil && os.SameFile(nullInfo, info) {
		return false
	}
	// the fallback when the null device could not be stat'ed: a stream opened on it by name, named
	return file.Name() != os.DevNull
}

// flat puts a call or its result on one short line: the line log is read as a list, not as a document.
func flat(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + "..."
	}
	return s
}
