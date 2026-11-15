package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// View is one consumer of the agent's events: thinking and tool progress go to the log,
// answers to stdout, so redirecting stdout stays clean. It answers nothing: a run with no screen
// has nobody at the keyboard.
type View struct {
	mu       sync.Mutex // tools may report from several goroutines
	out      io.Writer
	log      io.Writer
	colored  bool
	thinking bool
	answered bool
	// failed is the first write that did not land. Every write here is checked and kept rather than
	// dropped, because a run piped into something that stops reading early loses its answer in silence:
	// the writer fails, nothing is said, and the exit code says the task went fine. One error is enough
	// to say so with - after the first EPIPE every later write fails the same way.
	failed error
}

func NewView(out, log io.Writer) *View {
	return &View{out: out, log: log, colored: isTerminal(log)}
}

// Err is the first write that failed, nil when everything reached its stream. A caller that can report
// a failure - the single-task path, which has an exit code to set - asks this after the run.
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

// nullInfo is what the null device looks like, taken once: a stream that is the null device is a
// character device and so passes the mode test, but nobody reads what is written to it and nothing
// arrives on the one read from it. That is the opposite of interactive, and it is what a test binary or
// a `ykode < /dev/null` run hands us.
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
	// a stream opened on the null device by name is the same thing even where the two stats do not
	// agree on identity, which is how the null device behaves on windows
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
