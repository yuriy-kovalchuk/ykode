package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/cli"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/tui"
)

// FrontEnd is what the run reports to: the screen, or the plain stream for one task.
type FrontEnd struct {
	View agent.Sink
	hub  *tui.Hub
	// plain is the stream front end, kept so the run can ask whether its writes reached anything
	plain *cli.View
}

func NewFrontEnd(cfg config.Config) FrontEnd {
	if cfg.TUI {
		hub := tui.NewHub() // the screen answers confirmations itself
		return FrontEnd{View: hub, hub: hub}
	}
	view := cli.NewView(os.Stdout, os.Stderr)
	return FrontEnd{View: view, plain: view}
}

// run hands the agent to the session the flags asked for.
func (f FrontEnd) Run(a *agent.Agent, cfg config.Config, ctxSize int) error {
	if cfg.Prompt != "" {
		return answerOnce(a, cfg, f.plain)
	}
	return tui.Run(context.Background(), a, f.hub, cfg)
}

// answerOnce runs one task to completion, for scripts. The answer goes to stdout and the log to stderr,
// and a script judges the run by its exit code - so a write that did not land is a failure of the run,
// not decoration to be lost. A broken pipe on stdout is the exception the runtime handles its own way,
// by killing the process with SIGPIPE, which is at least an exit code a script can branch on.
func answerOnce(a *agent.Agent, cfg config.Config, view *cli.View) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := a.Ask(ctx, cfg.Prompt)
	if err != nil {
		return err
	}
	if view != nil {
		// the answer was produced and could not be delivered: say what failed, on the stream that may
		// still work, and fail the run rather than reporting success
		if werr := view.Err(); werr != nil {
			return fmt.Errorf("the answer could not be written: %w", werr)
		}
	}
	return nil
}
