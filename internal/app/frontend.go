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

// FrontEnd is what the run reports to: the screen, or the plain stream for one task.
type FrontEnd struct {
	View agent.Sink
	hub  *tui.Hub
}

func NewFrontEnd(cfg config.Config) FrontEnd {
	if cfg.TUI {
		hub := tui.NewHub() // the screen answers confirmations itself
		return FrontEnd{View: hub, hub: hub}
	}
	return FrontEnd{View: cli.NewView(os.Stdout, os.Stderr)}
}

// run hands the agent to the session the flags asked for.
func (f FrontEnd) Run(a *agent.Agent, cfg config.Config, ctxSize int) error {
	if cfg.Prompt != "" {
		return answerOnce(a, cfg)
	}
	return tui.Run(context.Background(), a, f.hub, cfg)
}

// answerOnce runs one task to completion, for scripts.
func answerOnce(a *agent.Agent, cfg config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return a.Ask(ctx, cfg.Prompt)
}
