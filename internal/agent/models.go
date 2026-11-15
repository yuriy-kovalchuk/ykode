package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

const modelsTimeout = 10 * time.Second // /model asks the server, and a server that is loading a model may be slow to answer

// modelCommand answers /model: with no argument, every model the server offers with the one in use
// marked; with an id or the number from that list, a switch to it. A model the server does not list is
// still accepted, because a proxy may load models it does not advertise.
func (a *Agent) modelCommand(ctx context.Context, arg string) string {
	models, err := a.ServerModels(ctx)
	if arg == "" {
		if err != nil {
			return fmt.Sprintf("model: %s (the server did not list its models: %v)\n", a.model, err)
		}
		return a.modelList(models)
	}
	chosen, listed := a.resolve(ctx, arg)
	a.UseModel(chosen, listed)
	switch {
	case !listed && err == nil:
		// an unlisted model carries no window. The one already in use is kept rather than replaced by
		// "unknown": a window of unknown switches the budget off and lets the history grow until the
		// server refuses, which is worse than a guess that was working. The line says which number is
		// being kept, so the guess is not mistaken for the model's own size.
		return fmt.Sprintf("model: %s (the server does not list it; keeping the %s window from the model you left; "+
			"the next request asks for it anyway)\n", chosen.ID, windowText(a.opts.CtxSize))
	case !listed:
		return fmt.Sprintf("model: %s (the server did not list its models, so it is unchecked; keeping the %s window; "+
			"the next request asks for it)\n", chosen.ID, windowText(a.opts.CtxSize))
	}
	return fmt.Sprintf("model: %s · window %s (the next request asks for it; a model the server has to load waits before the first token)\n",
		chosen.ID, windowText(a.windowFor(chosen, listed)))
}

// modelList is the server's models, numbered so a number can pick one.
func (a *Agent) modelList(models []llm.ModelInfo) string {
	width := 0
	for _, m := range models {
		width = max(width, len(m.ID))
	}
	rows := []string{fmt.Sprintf("%d model(s) on this server · * is the one in use · /model ID or /model NUMBER switches", len(models))}
	for i, m := range models {
		mark := " "
		if m.ID == a.model {
			mark = "*"
		}
		rows = append(rows, fmt.Sprintf(" %2d %s %-*s  %s", i+1, mark, width, m.ID, windowText(m.CtxSize)))
	}
	listed := false
	for _, m := range models {
		listed = listed || m.ID == a.model
	}
	if !listed {
		rows = append(rows, fmt.Sprintf("    * %s (in use, but this server does not list it)", a.model))
	}
	return strings.Join(rows, "\n") + "\n"
}

// ServerModels lists what this run's server offers, for a front end that shows them to choose from.
func (a *Agent) ServerModels(ctx context.Context) ([]llm.ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	return a.client.Models(ctx)
}

// resolve finds the model asked for: a number from the server's list, an id the server lists, or an id
// it does not, which a proxy may still load.
func (a *Agent) resolve(ctx context.Context, arg string) (llm.ModelInfo, bool) {
	models, _ := a.ServerModels(ctx)
	chosen, listed := llm.ModelInfo{ID: arg}, false
	if n, convErr := strconv.Atoi(arg); convErr == nil && n >= 1 && n <= len(models) {
		chosen, listed = models[n-1], true
	}
	for _, m := range models {
		if m.ID == arg {
			chosen, listed = m, true
		}
	}
	return chosen, listed
}

// UseModel points the run at a model, bringing its own window unless --ctx fixed one. A front end that
// listed the models asks for the switch without asking the server again.
func (a *Agent) UseModel(model llm.ModelInfo, listed bool) {
	a.SetEndpoint(a.client, model.ID, a.windowFor(model, listed))
}

func (a *Agent) windowFor(model llm.ModelInfo, listed bool) int {
	if listed && model.CtxSize > 0 && !a.opts.CtxPinned {
		return model.CtxSize
	}
	return a.opts.CtxSize
}

func windowText(tokens int) string {
	switch {
	case tokens <= 0:
		return "window unknown"
	case tokens >= 1000:
		return fmt.Sprintf("%dk window", tokens/1000)
	}
	return fmt.Sprintf("%d window", tokens)
}
