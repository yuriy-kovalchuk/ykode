package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// Renderer streams thinking to the log and the answer to stdout, so redirecting stdout stays clean.
type Renderer struct {
	out      io.Writer
	log      io.Writer
	colored  bool
	thinking bool
	answered bool
}

func NewRenderer(out, log io.Writer) *Renderer {
	return &Renderer{out: out, log: log, colored: isTerminal(log)}
}

func (r *Renderer) Reasoning(chunk string) {
	if !r.thinking {
		fmt.Fprint(r.log, r.dim()+"thinking: ")
		r.thinking = true
	}
	fmt.Fprint(r.log, chunk)
}

func (r *Renderer) Content(chunk string) {
	r.endThinking()
	fmt.Fprint(r.out, chunk)
	r.answered = true
}

func (r *Renderer) TurnDone(stats agent.Stats) {
	r.endThinking()
	if r.answered {
		fmt.Fprintln(r.out)
		r.answered = false
	}
	if stats.PromptTokens == 0 && stats.OutputTokens == 0 {
		return // server gave no timings
	}
	fmt.Fprintf(r.log, "  .. in %d (cached %d) / out %d · prefill %.0f tok/s · decode %.0f tok/s · %s\n",
		stats.PromptTokens, stats.CachedTokens, stats.OutputTokens,
		stats.PrefillTokSec, stats.DecodeTokSec, stats.FinishReason)
}

func (r *Renderer) endThinking() {
	if r.thinking {
		fmt.Fprint(r.log, r.reset()+"\n")
		r.thinking = false
	}
}

func (r *Renderer) dim() string {
	if r.colored {
		return "\x1b[2;3m" // dim + italic
	}
	return ""
}

func (r *Renderer) reset() string {
	if r.colored {
		return "\x1b[0m"
	}
	return ""
}

func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
