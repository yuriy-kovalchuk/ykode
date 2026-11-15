// Package app is ykode from settings to session; it also builds what the eval runs, so the two cannot drift.
package app

import (
	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// Options builds the tool and agent options for one run; the caller adds instructions and session.
func Options(cfg config.Config, model string, ctxSize int) (tools.Options, agent.Options) {
	resultBytes := tools.ResultBytesFor(ctxSize)
	thinking, noThink := cfg.SamplingFor(model)
	plan := tools.NewPlan() // one list: the tool writes it while the screen reads it
	return tools.Options{
		ReadLines:      cfg.ReadLines,
		ExecTimeout:    cfg.ExecTimeout,
		CommandLogDir:  cfg.CommandLogDir,
		MaxResultBytes: resultBytes,
		Plan:           plan,
	}, agent.Options{
		MaxTurns:         cfg.MaxTurns,
		MaxTokens:        cfg.MaxTokens,
		SafetyMargin:     cfg.SafetyMargin,
		CtxSize:          ctxSize,
		// the number typed on the command line is a request about this server, not about whichever model
		// gets picked next: without this, a model switch replaces what the user wrote with the model's own
		// window and says nothing.
		CtxPinned:        cfg.CtxSize > 0,
		KeepRecentTokens: min(20000, ctxSize/5),
		ThinkingLevel:    cfg.ThinkingLevel,
		AssumeYes:        cfg.AssumeYes,
		SendReasoning:    cfg.SendReasoningFor(model),
		Thinking:         thinking,
		NoThink:          noThink,
		MaxResult:        resultBytes,
		Allow:            cfg.Allow,
		Root:             cfg.Root,
		Plan:             plan,
	}
}
