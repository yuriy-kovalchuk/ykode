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

// inputModes is what the model takes besides text, which decides whether an attachment goes out
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
		// an attachment this model cannot take stays out of the request but in the conversation
		Modalities: a.inputModes(),
		// the screen shows the speed while the reply streams; servers that do not know the field ignore it
		TimingsPerToken: true,
		// a cold prompt of a long conversation reads for minutes: this says how far, and keeps the stream awake
		ReturnProgress: true,
	}
	req.Sampling = a.opts.Thinking
	level := a.ThinkingLevel()
	if level == llm.ThinkingOff {
		req.Sampling = a.opts.NoThink
		// llama.cpp honours "enable_thinking" and silently ignores "thinking_enabled"
		req.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
	}
	if a.opts.CtxSize > 0 {
		if used := a.estimateTokens(); a.usable()-used < minResponseTok {
			// the reserve is named: without it a window that is part empty reads as a miscount
			return llm.ChatRequest{}, fmt.Errorf("conversation is about %d tokens but the server context is %d, "+
				"%d of it held back for the answer (context.safety_margin); use /reset, /compact or raise --ctx",
				used, a.opts.CtxSize, a.reserve())
		}
	}
	req.MaxTokens = a.replyRoom()
	if level != llm.ThinkingOff {
		req.ThinkingBudget = thinkingWithin(level, req.MaxTokens)
	}
	return req, nil
}

// replyRoom is the cap a reply gets: the one asked for, or less when the window has less left; 0 is none
func (a *Agent) replyRoom() int {
	if a.opts.CtxSize <= 0 {
		return a.opts.MaxTokens
	}
	// no cap asked for: the room the window has left is the cap, so a reply cannot run past the prompt
	room := a.usable() - a.estimateTokens()
	if a.opts.MaxTokens == 0 || room < a.opts.MaxTokens {
		return room
	}
	return a.opts.MaxTokens
}

// thinkingWithin is a level's budget cut to what a reply of reply tokens can spare; 0 asks for no cap
func thinkingWithin(level string, reply int) int {
	budget, _ := llm.ThinkingBudgetTokens(level)
	if budget <= 0 || reply <= 0 {
		return budget
	}
	// thinking is billed inside the reply, so it never takes more than the reply can spare
	budget = min(budget, reply-minAnswerTok)
	if budget <= 0 {
		// a budget of 0 is omitted on the wire, and an omitted budget means no limit at all
		budget = max(reply/2, 1)
	}
	return budget
}

// ThinkingBudget is what the next request would ask a level to think within, after the window's cut
func (a *Agent) ThinkingBudget(level string) int { return thinkingWithin(level, a.replyRoom()) }

// reserve is the room held for the reply and template, never over a quarter of the window
func (a *Agent) reserve() int {
	return min(cmp.Or(a.opts.SafetyMargin, ctxSafetyTok), max(a.opts.CtxSize/4, ctxSafetyTok))
}

// usable is the window less the reserve: what a request may fill, reply included.
func (a *Agent) usable() int { return a.opts.CtxSize - a.reserve() }

// the conversation stays under two marks: the ceiling is where the oldest messages get dropped
func (a *Agent) ceiling() int {
	return min(a.opts.CtxSize*9/10, a.usable()-minResponseTok)
}

// pressureMark is the last point a fold still fits, since a fold pays for turns and recap alike
func (a *Agent) pressureMark() int {
	return min(a.ceiling(), a.usable()-a.compactRoom())
}

// underPressure says the conversation passed the mark where context starts being worth money.
func (a *Agent) underPressure() bool {
	return a.opts.CtxSize > 0 && a.estimateTokens() > a.pressureMark()
}

// neverRan is the reply written for a call that was interrupted before it answered.
const neverRan = "this call never ran: the request was interrupted before it finished"

// sanitizeHistory keeps history a server accepts: a cut-off call or a missing reply fails the request
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
	// a repaired call's arguments go to the log as one line each, not as a copy of the conversation
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
