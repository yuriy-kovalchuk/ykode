package cli

import (
	"fmt"
	"io"
	"strconv"
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

// Answer puts the model's question and its choices to the person and reads one line back: a bare
// number picks the choice it names, and an empty line or an end of input says there is no answer.
func (c *Confirmer) Answer(question string, options []string) string {
	fmt.Fprintf(c.out, "\n%s\n", question)
	for i, option := range options {
		fmt.Fprintf(c.out, "  %d. %s\n", i+1, option)
	}
	fmt.Fprint(c.out, "your answer [Enter for none] ")
	answer, ok := c.lines.next()
	if !ok {
		fmt.Fprintln(c.out)
		return ""
	}
	answer = strings.TrimSpace(answer)
	if number, err := strconv.Atoi(answer); err == nil && number >= 1 && number <= len(options) {
		return options[number-1] // the model reads the choice in words, not the number it was picked by
	}
	return answer
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
