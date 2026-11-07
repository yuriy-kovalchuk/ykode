// Package cli is the terminal front end: line reader, confirmation prompt, read-eval loop.
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

// LineSource reads stdin on one goroutine so prompt and confirmation never fight; typed lines are buffered.
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

// readLine waits for a line, giving up on Ctrl-C, SIGTERM or ctx so the prompt stays interruptible.
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

// Run reads until EOF, /exit or Ctrl-C at the prompt; mid-request Ctrl-C cancels the request.
func (r *REPL) Run(ctx context.Context) error {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)

	fmt.Fprintln(r.out, `ykode: ask for a change, "/help" lists the commands, "/exit" quits.`)
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

// command hands a slash command to the agent and prints what it answers.
func (r *REPL) command(line string) (quit bool, err error) {
	text, quit, err := r.agent.Command(line)
	fmt.Fprint(r.out, text)
	return quit, err
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
			// the turn may have finished meanwhile, so the signal was meant for the prompt
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
