package agent

import "github.com/yuriy-kovalchuk/ykode/internal/llm"

// Steer offers an ask to the run in progress: it joins the conversation at the next round-trip
// rather than waiting for the whole turn to end. The round-trip already in flight and the tools
// already running are not interrupted.
func (a *Agent) Steer(text string) {
	a.waitingMu.Lock()
	defer a.waitingMu.Unlock()
	a.waiting = append(a.waiting, text)
}

// Waiting is what has been typed but has not reached the model yet, for a front end to keep on
// screen and, once the run ends, to start as an ordinary prompt.
func (a *Agent) Waiting() []string {
	a.waitingMu.Lock()
	defer a.waitingMu.Unlock()
	return append([]string(nil), a.waiting...)
}

// NextWaiting takes the oldest ask still waiting, for a front end that starts it as a prompt.
func (a *Agent) NextWaiting() (string, bool) {
	a.waitingMu.Lock()
	defer a.waitingMu.Unlock()
	if len(a.waiting) == 0 {
		return "", false
	}
	ask := a.waiting[0]
	a.waiting = a.waiting[1:]
	return ask, true
}

// takeWaiting is the loop's seam: everything typed since the last round-trip, in the words the
// model reads.
func (a *Agent) takeWaiting() []llm.Message {
	a.waitingMu.Lock()
	defer a.waitingMu.Unlock()
	asks := a.waiting
	a.waiting = nil
	messages := make([]llm.Message, 0, len(asks))
	for _, ask := range asks {
		messages = append(messages, llm.Message{Role: "user", Content: ask})
	}
	return messages
}

// dropWaiting forgets what was waiting: a conversation starting over has nothing left to steer.
func (a *Agent) dropWaiting() {
	a.waitingMu.Lock()
	defer a.waitingMu.Unlock()
	a.waiting = nil
}
