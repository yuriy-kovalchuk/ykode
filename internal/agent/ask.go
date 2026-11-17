package agent

import (
	"context"

	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// asksTo is how ask_user reaches the person running ykode: through the sink that draws the run
type asksTo struct{ a *Agent }

func (s asksTo) Ask(ctx context.Context, q tools.Question) string {
	asker, ok := s.a.sink.(Asker)
	if !ok {
		return "" // a model is offered the tool only when its front end can answer
	}
	return asker.Ask(ctx, Event{Kind: Ask, Turn: s.a.turnNo(), Text: q.Text, Options: q.Options})
}
