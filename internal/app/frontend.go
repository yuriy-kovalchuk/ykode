package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/cli"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/tui"
)

// FrontEnd is what the run reports to and reads from: the line prompt, the screen, or JSON lines.
type FrontEnd struct {
	View   agent.Sink
	lines  *cli.LineSource
	events *cli.JSONWriter
	hub    *tui.Hub
}

func NewFrontEnd(cfg config.Config) FrontEnd {
	var f FrontEnd
	// Only the line front end needs a reader of its own: the screen reads the terminal itself,
	// and a second reader of stdin would swallow what the user types into it.
	var confirm *cli.Confirmer
	if readsStdinLineByLine(cfg) {
		f.lines = cli.NewLineSource(os.Stdin)
		// -p with a pipe has nobody to answer a confirmation, so nothing outside the defaults may run
		if cfg.Prompt == "" || cli.IsTerminal(os.Stdin) {
			confirm = cli.NewConfirmer(f.lines, os.Stdout)
		}
	}
	f.View = cli.NewView(os.Stdout, os.Stderr, confirm)
	if cfg.TUI {
		f.hub = tui.NewHub() // the screen answers confirmations itself
		f.View = f.hub
	}
	if cfg.JSON {
		f.events = cli.NewJSONWriter(os.Stdout)
		f.View = f.events
	}
	return f
}

// run hands the agent to the session the flags asked for.
func (f FrontEnd) Run(a *agent.Agent, cfg config.Config, ctxSize int) error {
	switch {
	case cfg.Prompt != "":
		return answerOnce(a, f.events, cfg, ctxSize)
	case cfg.TUI:
		return tui.Run(context.Background(), a, f.hub)
	default:
		return cli.NewREPL(a, f.lines, os.Stdout, os.Stderr).Run(context.Background())
	}
}

// readsStdinLineByLine says whether the front end reads stdin a line at a time.
func readsStdinLineByLine(cfg config.Config) bool { return !cfg.TUI }

// answerOnce runs one task to completion, for scripts and the eval harness.
func answerOnce(a *agent.Agent, events *cli.JSONWriter, cfg config.Config, ctxSize int) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := a.Ask(ctx, cfg.Prompt)
	if events != nil {
		if err != nil {
			events.Failure(err)
		}
		events.Usage(a.Usage(), ctxSize)
	}
	return err
}
