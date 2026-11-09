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
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	models, err := a.client.Models(ctx)
	if arg == "" {
		if err != nil {
			return fmt.Sprintf("model: %s (the server did not list its models: %v)\n", a.model, err)
		}
		return a.modelList(models)
	}
	chosen := llm.ModelInfo{ID: arg}
	listed := false
	if n, convErr := strconv.Atoi(arg); convErr == nil && n >= 1 && n <= len(models) {
		chosen, listed = models[n-1], true
	}
	for _, m := range models {
		if m.ID == arg {
			chosen, listed = m, true
		}
	}
	window := a.opts.CtxSize
	if listed && chosen.CtxSize > 0 && !a.opts.CtxPinned {
		window = chosen.CtxSize // another model brings its own window, unless --ctx fixed one
	}
	a.SetEndpoint(a.client, chosen.ID, window)
	switch {
	case !listed && err == nil:
		return fmt.Sprintf("model: %s (the server does not list it; the next request asks for it anyway)\n", chosen.ID)
	case !listed:
		return fmt.Sprintf("model: %s (the server did not list its models, so it is unchecked; the next request asks for it)\n", chosen.ID)
	}
	return fmt.Sprintf("model: %s · window %s (the next request asks for it; a model the server has to load waits before the first token)\n",
		chosen.ID, windowText(window))
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

func windowText(tokens int) string {
	switch {
	case tokens <= 0:
		return "window unknown"
	case tokens >= 1000:
		return fmt.Sprintf("%dk window", tokens/1000)
	}
	return fmt.Sprintf("%d window", tokens)
}
