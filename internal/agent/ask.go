package agent

import (
	"context"

	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// asksTo is how the ask_user tool reaches the person running ykode: the same sink that draws the run
// answers the question, in words. Empty says there is none: the run was cancelled, the screen closed,
// or the front end has nobody watching it. The run keeps its place while it waits, the way a
// confirmation does.
type asksTo struct{ a *Agent }

func (s asksTo) Ask(ctx context.Context, q tools.Question) string {
	asker, ok := s.a.sink.(Asker)
	if !ok {
		return "" // a model is offered the tool only when its front end can answer
	}
	return asker.Ask(ctx, Event{Kind: Ask, Turn: s.a.turnNo(), Text: q.Text, Options: q.Options})
}
