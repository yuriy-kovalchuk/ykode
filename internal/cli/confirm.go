package cli

import (
	"fmt"
	"io"
	"strings"
)

// Confirmer asks before a mutating tool runs. It reads from the REPL's
// LineSource, so stdin is only ever read in one place.
//
// Ctrl-C is deliberately not honoured mid-question (the tool has not run yet):
// answer "n", or press Ctrl-D, to refuse.
type Confirmer struct {
	lines *LineSource
	out   io.Writer
}

func NewConfirmer(lines *LineSource, out io.Writer) *Confirmer {
	return &Confirmer{lines: lines, out: out}
}

// Ask prints "<what> [y/N] " and reports whether the user agreed. Anything
// other than y/yes, including EOF, means no.
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
