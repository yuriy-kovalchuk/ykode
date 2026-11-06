package agent

import (
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

const (
	summaryFloor = 400 // tool results smaller than this are left alone
	keepResults  = 2   // the two newest results stay word for word
	shrinkMark   = "[ykode shortened this"
)

// underPressure says the conversation passed 3/4 of the server window, where context starts being worth money.
func (a *Agent) underPressure() bool {
	return a.opts.CtxSize > 0 && a.estimateTokens() > a.opts.CtxSize*3/4
}

// stillTight says shortening the results did not buy enough room, which is when compaction earns its call.
func (a *Agent) stillTight() bool {
	return a.opts.CtxSize > 0 && a.estimateTokens() > a.opts.CtxSize*3/4
}

// shrinkOldResults replaces older large tool results with a summary the code writes, and returns the bytes freed.
func (a *Agent) shrinkOldResults(keep int) int {
	var positions []int
	for i, m := range a.history {
		if m.Role == "tool" {
			positions = append(positions, i)
		}
	}
	freed := 0
	for n, i := range positions {
		if n >= len(positions)-keep {
			break // the model is still working with these
		}
		m := &a.history[i]
		if m.CallID == "" || len(m.Content) <= summaryFloor || strings.HasPrefix(m.Content, shrinkMark) {
			continue
		}
		summary := a.summarize(m, m.CallID)
		freed += len(m.Content) - len(summary)
		m.Content = summary
		a.shortened = true
	}
	return freed
}

// summarize is what ykode keeps of a dropped result: which call it was, how big it was, how it began, and where the text is.
func (a *Agent) summarize(m *llm.Message, id string) string {
	name := m.Name
	if name == "" {
		name = "tool"
	}
	text := fmt.Sprintf("%s %s output (it was %d bytes) to fit the context; it began: %q",
		shrinkMark, name, len(m.Content), oneLine(m.Content, 120))
	if !a.opts.Store.Saved() {
		return text + "]"
	}
	if err := a.opts.Store.Save(id, m.Content); err != nil {
		a.tell(fmt.Sprintf("could not keep the full %s output: %v", name, err))
		return text + "]"
	}
	return fmt.Sprintf("%s: recall {\"id\":\"%s\"} hands back the full text]", text, id)
}
