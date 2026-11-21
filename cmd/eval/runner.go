package main

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/app"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// defaultMaxTurns bounds a task: anything longer has usually stopped looping.
const defaultMaxTurns = 8

func runTask(ctx context.Context, client *llm.Client, model llm.ModelInfo, cfg config.Config, t task) evalResult {
	base, err := os.MkdirTemp("", "ykode-eval-")
	if err != nil {
		return evalResult{name: t.name, note: err.Error()}
	}
	project := filepath.Join(base, "project")
	if err := writeFixture(base, t.fixture, t.outside); err != nil {
		return evalResult{name: t.name, note: err.Error(), dir: base}
	}
	root, err := os.OpenRoot(project)
	if err != nil {
		return evalResult{name: t.name, note: err.Error(), dir: base}
	}
	defer func() { _ = root.Close() }() // a read-only handle on a temp folder has nothing to report

	// a task can ask for a smaller window than the server offers, which is how folding gets exercised at all
	ctxSize := model.CtxSize
	if t.ctx > 0 {
		ctxSize = t.ctx
	}
	instructions, _ := agent.LoadInstructions("", project, agent.InstructionBytes(ctxSize))
	rec := newRecorder()
	cfg.Root = project
	toolOpts, agentOpts := app.Options(cfg, model.ID, ctxSize)
	agentOpts.Vision = model.Vision
	agentOpts.MaxTurns = cmp.Or(t.maxTurns, defaultMaxTurns)
	agentOpts.AssumeYes = true // nobody to ask: in-project work runs, outside work is refused
	agentOpts.Instructions = instructions
	a := agent.New(client, tools.BuiltIn(root, project, toolOpts), model.ID, agentOpts, rec)

	start := time.Now()
	askErr := a.Ask(ctx, t.prompt)
	if askErr == nil && t.followUp != "" {
		askErr = a.Ask(ctx, t.followUp)
	}
	usage := a.Usage()
	rec.lastAnswer()

	passed, note := t.check(project, &rec.transcript, askErr)
	if askErr != nil && passed {
		note = askErr.Error() + "; " + note
	}
	return evalResult{
		name:      t.name,
		passed:    passed,
		turns:     usage.Turns,
		prompt:    usage.PromptTok,
		generated: usage.GenTok,
		seconds:   time.Since(start).Seconds(),
		note:      note,
		story:     rec.transcript.describe(),
		dir:       base,
		counters:  rec.counters(askErr),
	}
}
