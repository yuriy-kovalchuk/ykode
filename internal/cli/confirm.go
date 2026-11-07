package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// Confirmer asks before a mutating tool runs, reading the REPL's LineSource; "n" or Ctrl-D refuses.
type Confirmer struct {
	lines *LineSource
	out   io.Writer
}

func NewConfirmer(lines *LineSource, out io.Writer) *Confirmer {
	return &Confirmer{lines: lines, out: out}
}

// Ask prints "<what> [y/N/a] "; anything other than y/yes/a, including EOF, means no.
// "a" means yes, and stop asking about this tool or this command prefix for the rest of the run.
func (c *Confirmer) Ask(what string) agent.Decision {
	fmt.Fprintf(c.out, "%s [y/N/a] ", what)
	answer, ok := c.lines.next()
	if !ok {
		fmt.Fprintln(c.out)
		return agent.Refuse
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return agent.Allow
	case "a", "always":
		return agent.AllowAlways
	default:
		return agent.Refuse
	}
}
