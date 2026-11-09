// Package app is ykode from settings to session; it also builds what the eval runs, so the two cannot drift.
package app

import (
	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// Options builds the tool and agent options for one run; the caller adds instructions, audit and session.
func Options(cfg config.Config, model string, ctxSize int, store tools.ResultStore, undo *tools.UndoStack) (tools.Options, agent.Options) {
	resultBytes := tools.ResultBytesFor(ctxSize)
	thinking, noThink := cfg.SamplingFor(model)
	plan := tools.NewPlan() // one list: the tool writes it while the screen reads it
	return tools.Options{
		ReadLines:      cfg.ReadLines,
		SearchLimit:    cfg.SearchLimit,
		ExecTimeout:    cfg.ExecTimeout,
		CommandLogDir:  cfg.CommandLogDir,
		MaxResultBytes: resultBytes,
		Store:          store,
		Undo:           undo,
		Plan:           plan,
		Sandbox:        cfg.Sandbox,
	}, agent.Options{
		MaxTurns:         cfg.MaxTurns,
		MaxTokens:        cfg.MaxTokens,
		SafetyMargin:     cfg.SafetyMargin,
		CtxSize:          ctxSize,
		KeepRecentTokens: min(20000, ctxSize/5),
		ThinkingLevel:    cfg.ThinkingLevel,
		AssumeYes:        cfg.AssumeYes,
		NoVerify:         cfg.NoVerify,
		SendReasoning:    cfg.SendReasoningFor(model),
		Thinking:         thinking,
		NoThink:          noThink,
		MaxResult:        resultBytes,
		Store:            store,
		Undo:             undo,
		Allow:            cfg.Allow,
		Root:             cfg.Root,
		Plan:             plan,
	}
}
