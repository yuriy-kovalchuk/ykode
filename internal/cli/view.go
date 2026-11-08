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
// answers to stdout, so redirecting stdout stays clean.
type View struct {
	mu       sync.Mutex // tools may report from several goroutines
	out      io.Writer
	log      io.Writer
	colored  bool
	confirm  *Confirmer
	thinking bool
	answered bool
}

func NewView(out, log io.Writer, confirm *Confirmer) *View {
	return &View{out: out, log: log, colored: isTerminal(log), confirm: confirm}
}

// Emit renders one event; only a Confirm event cares about what comes back.
func (v *View) Emit(ev agent.Event) agent.Decision {
	v.mu.Lock()
	defer v.mu.Unlock()

	switch ev.Kind {
	case agent.Thinking:
		if !v.thinking {
			fmt.Fprint(v.log, v.dim()+"thinking: ")
			v.thinking = true
		}
		fmt.Fprint(v.log, ev.Text)
	case agent.Answer:
		v.endThinking()
		fmt.Fprint(v.out, ev.Text)
		v.answered = true
	case agent.TurnEnd:
		v.endThinking()
		if v.answered {
			fmt.Fprintln(v.out)
			v.answered = false
		}
		if ev.Stats.PromptTokens == 0 && ev.Stats.OutputTokens == 0 {
			return agent.Allow // server gave no timings
		}
		fmt.Fprintf(v.log, "  .. in %d (cached %d) / out %d · prefill %.0f tok/s · decode %.0f tok/s · %s\n",
			ev.Stats.PromptTokens, ev.Stats.CachedTokens, ev.Stats.OutputTokens,
			ev.Stats.PrefillTokSec, ev.Stats.DecodeTokSec, ev.Stats.FinishReason)
	case agent.ToolStart:
		v.endThinking()
		fmt.Fprintf(v.log, "  -> %s %s\n", ev.Tool, ev.Args)
	case agent.ToolEnd:
		fmt.Fprintf(v.log, "  <- %s %s\n", ev.Tool, ev.Text)
	case agent.ToolFail:
		fmt.Fprintf(v.log, "  !! %s: %s\n", ev.Tool, ev.Text)
	case agent.Summary:
		v.endThinking()
		fmt.Fprintln(v.log, "  .. the older turns, folded into:")
		for _, line := range strings.Split(ev.Text, "\n") {
			fmt.Fprintf(v.log, "     %s\n", line)
		}
	case agent.Notice:
		fmt.Fprintf(v.log, "  .. %s\n", ev.Text)
	case agent.Confirm:
		if v.confirm == nil {
			return agent.Refuse
		}
		return v.confirm.Ask(ev.Text)
	}
	return agent.Allow
}

func (v *View) endThinking() {
	if v.thinking {
		fmt.Fprint(v.log, v.reset()+"\n")
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

func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
