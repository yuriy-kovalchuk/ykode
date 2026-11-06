// Package cli is the terminal front end: a line reader, a confirmation prompt
// and the read-eval loop around the agent.
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// LineSource reads stdin on one background goroutine so the prompt and the
// confirmation prompt never fight over the reader. A line typed while the agent
// is working is buffered and picked up by the next prompt or confirmation.
type LineSource struct {
	ch chan string
}

func NewLineSource(r io.Reader) *LineSource {
	ls := &LineSource{ch: make(chan string)}
	go func() {
		defer close(ls.ch)
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			ls.ch <- scanner.Text()
		}
	}()
	return ls
}

// next returns the next typed line; ok is false at EOF.
func (l *LineSource) next() (string, bool) {
	line, ok := <-l.ch
	return line, ok
}

// readLine waits for a typed line, giving up on Ctrl-C, SIGTERM or ctx
// cancellation so the prompt itself stays interruptible.
func (l *LineSource) readLine(ctx context.Context, sigs chan os.Signal) (line string, ok bool, interrupted bool) {
	select {
	case <-ctx.Done():
		return "", false, true
	case <-sigs:
		return "", false, true
	case text, more := <-l.ch:
		return text, more, false
	}
}

// REPL is the interactive prompt.
type REPL struct {
	agent *agent.Agent
	lines *LineSource
	out   io.Writer // answers and prompts
	log   io.Writer // errors and tool progress
}

func NewREPL(a *agent.Agent, lines *LineSource, out, log io.Writer) *REPL {
	return &REPL{agent: a, lines: lines, out: out, log: log}
}

// Run reads requests until EOF, /exit, or Ctrl-C at the prompt. Signals are
// handled here: during a request Ctrl-C cancels it, at the prompt it quits.
func (r *REPL) Run(ctx context.Context) error {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)

	fmt.Fprintln(r.out, `ykode: ask for a change, "/reset" clears the history, "/exit" quits.`)
	for {
		fmt.Fprint(r.out, "> ")
		line, ok, interrupted := r.lines.readLine(ctx, sigs)
		if interrupted {
			fmt.Fprintln(r.out, "^C")
			return nil
		}
		if !ok {
			fmt.Fprintln(r.out)
			return nil
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line[0] == '/' {
			if quit, err := r.command(line); quit {
				return err
			}
			continue
		}
		if err := r.ask(ctx, sigs, line); err != nil {
			fmt.Fprintln(r.log, "error:", err)
		}
	}
}

// command handles a slash command and reports whether the REPL should stop.
func (r *REPL) command(line string) (quit bool, err error) {
	switch line {
	case "/exit", "/quit":
		return true, nil
	case "/reset":
		r.agent.Reset()
		fmt.Fprintln(r.out, "history cleared")
	default:
		fmt.Fprintf(r.log, "unknown command %q; try /reset or /exit\n", line)
	}
	return false, nil
}

// ask runs one agent turn, cancelling it if the user presses Ctrl-C.
func (r *REPL) ask(ctx context.Context, sigs chan os.Signal, prompt string) error {
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-done:
		case <-sigs:
			// The turn may have finished in the meantime; then the signal was
			// meant for the prompt and is left to the read loop.
			select {
			case <-done:
			default:
				fmt.Fprintln(r.log, "^C cancelling request")
				cancel()
			}
		}
	}()
	return r.agent.Ask(turnCtx, prompt)
}
