package cli

import (
	"fmt"
	"io"
	"strings"
)

// Confirmer asks before a mutating tool runs, reading the REPL's LineSource; "n" or Ctrl-D refuses.
type Confirmer struct {
	lines *LineSource
	out   io.Writer
}

func NewConfirmer(lines *LineSource, out io.Writer) *Confirmer {
	return &Confirmer{lines: lines, out: out}
}

// Ask prints "<what> [y/N] "; anything other than y/yes, including EOF, means no.
func (c *Confirmer) Ask(what string) bool {
	fmt.Fprintf(c.out, "%s [y/N] ", what)
	answer, ok := c.lines.next()
	if !ok {
		fmt.Fprintln(c.out)
		return false
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
