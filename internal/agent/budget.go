package agent

import (
	"cmp"
	"encoding/json"
	"fmt"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

const (
	ctxSafetyTok   = 256  // the least to leave free when the caller named no margin
	minResponseTok = 64   // a response budget smaller than this is useless
	minAnswerTok   = 1024 // what the answer keeps after a thinking phase takes its share
)

// inputModes is what the model in use takes as input besides text, which is what decides whether an
// attachment goes out at all. Text needs no saying: every chat server takes it.
func (a *Agent) inputModes() []string {
	if !a.opts.Vision {
		return nil
	}
	return []string{llm.ModalityImage}
}

// request builds the chat body and leaves the response the room the context window still has.
func (a *Agent) request() (llm.ChatRequest, error) {
	a.sanitizeHistory()
	req := llm.ChatRequest{
		Model:      a.model,
		Messages:   a.history,
		Tools:      a.specs(),
		ToolChoice: "auto",
		MaxTokens:  a.opts.MaxTokens,
		// what this model is known to take: an attachment of anything else stays out of the request while
		// it stays in the conversation, so a model that reads it gets it back
		Modalities: a.inputModes(),
		// the screen shows the speed while the reply streams; servers that do not know the field ignore it
		TimingsPerToken: true,
	}
	req.Sampling = a.opts.Thinking
	if level := a.ThinkingLevel(); level == llm.ThinkingOff {
		req.Sampling = a.opts.NoThink
		// llama.cpp honours "enable_thinking" and silently ignores "thinking_enabled"
		req.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
	} else if budget, known := llm.ThinkingBudgetTokens(level); known {
		req.ThinkingBudget = budget
	}
	if a.opts.CtxSize > 0 {
		used := a.estimateTokens()
		room := a.usable() - used
		if room < minResponseTok {
			return llm.ChatRequest{}, fmt.Errorf(
				"conversation is about %d tokens but the server context is %d; use /reset or raise --ctx", used, a.opts.CtxSize)
		}
		// no cap asked for: the room the window has left is the cap, so a reply cannot run past the prompt
		if req.MaxTokens == 0 || room < req.MaxTokens {
			req.MaxTokens = room
		}
		// thinking is billed inside the reply, so it never takes more than the reply can spare
		if req.ThinkingBudget > 0 {
			req.ThinkingBudget = min(req.ThinkingBudget, req.MaxTokens-minAnswerTok)
			if req.ThinkingBudget <= 0 {
				// a budget of 0 is omitted on the wire, and an omitted budget means no limit at all
				req.ThinkingBudget = max(req.MaxTokens/2, 1)
			}
		}
	}
	return req, nil
}

// reserve is the part of the window held back for the reply and the chat template. It is what the
// user asked for, but never more than a quarter of the window: a reserve of four thousand tokens
// is sensible against a window of sixty thousand and is the whole of a window of four thousand, which
// is what a llama-server started with no flags has.
func (a *Agent) reserve() int {
	return min(cmp.Or(a.opts.SafetyMargin, ctxSafetyTok), max(a.opts.CtxSize/4, ctxSafetyTok))
}

// usable is the window less the reserve: what a request may fill, reply included.
func (a *Agent) usable() int { return a.opts.CtxSize - a.reserve() }

// The conversation is kept under two marks. The ceiling is where the oldest messages start being
// dropped whole: nine tenths of the window, or less when the reserve asks for more than that tenth -
// so what a drop leaves always passes the check a request makes, and a run never folds, drops and
// then refuses to send.
func (a *Agent) ceiling() int {
	return min(a.opts.CtxSize*9/10, a.usable()-minResponseTok)
}

// pressureMark, where the older turns are folded into a summary, is the last moment a fold is still
// possible: the fold's request carries the older turns and pays for the summary they become, so the
// ask belongs where the window can still hold both. Folding earlier throws away whole results that
// the model was still using; folding later leaves the drop layer to delete what a summary would have
// written down.
func (a *Agent) pressureMark() int {
	return min(a.ceiling(), a.usable()-a.compactRoom())
}

// underPressure says the conversation passed the mark where context starts being worth money.
func (a *Agent) underPressure() bool {
	return a.opts.CtxSize > 0 && a.estimateTokens() > a.pressureMark()
}

// neverRan is the reply written for a call that was interrupted before it answered.
const neverRan = "this call never ran: the request was interrupted before it finished"

// sanitizeHistory keeps history something a server accepts: a cut-off call holds invalid JSON, and a
// call whose reply was never written (the user pressed Ctrl-C mid-tool) makes the whole request fail.
func (a *Agent) sanitizeHistory() {
	fixed := []int{}
	for i := range a.history {
		for j := range a.history[i].ToolCalls {
			if args := a.history[i].ToolCalls[j].Function.Arguments; !json.Valid(args) {
				a.history[i].ToolCalls[j].Function.Arguments = json.RawMessage("{}")
				fixed = append(fixed, i)
			}
		}
	}
	// a call's arguments repaired in place is one line each, not a copy of the conversation: the trim and
	// the fold already write the whole thing often enough. Positions are only safe while the file holds
	// exactly this conversation less the system prompt, which the added replies below would move.
	if len(fixed) > 0 && a.opts.Session.Count() == len(a.history)-1 {
		for _, i := range fixed {
			if err := a.opts.Session.Revised(i-1, a.history[i]); err != nil {
				a.reportSave(a.opts.Session.Rewritten(a.history[1:]))
				break
			}
		}
	}

	added := false
	repaired := make([]llm.Message, 0, len(a.history))
	for i := 0; i < len(a.history); i++ {
		m := a.history[i]
		repaired = append(repaired, m)
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		answered := map[string]bool{}
		j := i + 1
		for ; j < len(a.history) && a.history[j].Role == "tool"; j++ {
			answered[a.history[j].ToolCallID] = true
			repaired = append(repaired, a.history[j])
		}
		for _, call := range m.ToolCalls {
			if !answered[call.ID] {
				added = true
				repaired = append(repaired, llm.Message{
					Role:       "tool",
					ToolCallID: call.ID,
					Name:       call.Function.Name,
					Content:    neverRan,
				})
			}
		}
		i = j - 1 // the replies that were already there have been copied
	}
	a.history = repaired
	a.recountBytes()
	if added {
		// messages moved: the saved conversation is written again whole, so its positions still match
		a.reportSave(a.opts.Session.Rewritten(a.history[1:]))
	}
}
