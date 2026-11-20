// Package tui is the full-screen front end: transcript, input box and status bar, driven by events
package tui

import (
	"context"
	"io"
	"os"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

// Hub is the agent's sink while the screen is up: reports queue here, the screen hears, answers wait
type Hub struct {
	done chan struct{} // closed when the screen is gone: nobody is left to answer a question

	mu      sync.Mutex
	send    func(tea.Msg) // nil until Run starts the program
	waiting []tea.Msg     // what the run reported since the screen last looked, oldest first
	told    bool          // the screen has been told to look and has not yet
}

func NewHub() *Hub { return &Hub{done: make(chan struct{})} }

// wire points the hub at the program that draws, set once before any run, so both take the lock
func (h *Hub) wire(send func(tea.Msg)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.send = send
}

// close says the screen is gone: a run still awaiting an answer gets none and ends
func (h *Hub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-h.done:
	default:
		close(h.done)
	}
}

// wakeMsg tells the screen the hub holds something for it.
type wakeMsg struct{}

// Emit forwards one event; only Confirm cares about what comes back.
func (h *Hub) Emit(ev agent.Event) agent.Decision {
	if ev.Kind != agent.Confirm {
		h.post(eventMsg{ev: ev})
		return agent.Allow
	}
	reply := make(chan agent.Decision, 1)
	if !h.post(confirmMsg{ev: ev, reply: reply}) {
		return agent.Refuse // nothing on screen to answer
	}
	select {
	case decision := <-reply:
		return decision
	case <-h.done:
		return agent.Refuse // the screen left with the question still on it
	}
}

// Ask forwards the model's question and waits for words; no screen or a cancel answers nothing
func (h *Hub) Ask(ctx context.Context, ev agent.Event) string {
	reply := make(chan string, 1)
	if !h.post(askMsg{ev: ev, reply: reply}) {
		return "" // nothing on screen to answer
	}
	select {
	case answer := <-reply:
		return answer
	case <-ctx.Done():
		return ""
	case <-h.done:
		return "" // the screen left with the question still on it
	}
}

// post queues a message for the screen and returns at once; false means nothing draws yet
func (h *Hub) post(msg tea.Msg) bool {
	h.mu.Lock()
	send := h.send
	if send == nil {
		h.mu.Unlock()
		return false
	}
	h.waiting = joined(h.waiting, msg)
	wake := !h.told
	h.told = true
	h.mu.Unlock()
	if wake {
		go send(wakeMsg{})
	}
	return true
}

// take hands over everything queued, in the order it was reported.
func (h *Hub) take() []tea.Msg {
	h.mu.Lock()
	defer h.mu.Unlock()
	waiting := h.waiting
	h.waiting, h.told = nil, false
	return waiting
}

// joined folds a message into the one before it when they say one thing, speeds folded last
func joined(queue []tea.Msg, msg tea.Msg) []tea.Msg {
	next, ok := msg.(eventMsg)
	if !ok {
		return append(queue, msg)
	}
	if next.ev.Kind == agent.Progress {
		for i, queued := range queue {
			if before, ok := queued.(eventMsg); ok && before.ev.Kind == agent.Progress {
				before.ev.Stats = newerSpeeds(before.ev.Stats, next.ev.Stats)
				queue[i] = before
				return queue
			}
		}
		return append(queue, msg)
	}
	last := len(queue) - 1
	for last >= 0 {
		before, ok := queue[last].(eventMsg)
		if !ok || before.ev.Kind != agent.Progress {
			break
		}
		last-- // the speeds are not text: what is joined is what stands before them
	}
	if last >= 0 {
		if before, ok := queue[last].(eventMsg); ok && continues(before.ev, next.ev) {
			before.ev.Text += next.ev.Text
			if next.ev.Tool != "" {
				before.ev.Tool = next.ev.Tool // a call's name can arrive in pieces too: the newest is the fullest
			}
			queue[last] = before
			return queue
		}
	}
	return append(queue, msg)
}

// continues says a streamed piece carries on the one before it.
func continues(before, next agent.Event) bool {
	if before.Kind != next.Kind {
		return false
	}
	switch next.Kind {
	case agent.Thinking, agent.Answer:
		return true
	case agent.ToolDraft:
		return before.Call == next.Call
	}
	return false
}

// newerSpeeds lays a later report over an earlier one; zero says nothing was said
func newerSpeeds(before, next agent.Stats) agent.Stats {
	if next.DecodeTokSec > 0 {
		before.DecodeTokSec, before.Estimated, before.OutputTokens = next.DecodeTokSec, next.Estimated, next.OutputTokens
	}
	if next.PrefillTokSec > 0 {
		before.PrefillTokSec = next.PrefillTokSec
	}
	return before
}

// Run draws the session until quit or ctx: the screen never takes the pointer, keys scroll
func Run(ctx context.Context, a *agent.Agent, hub *Hub, cfg config.Config) error {
	return run(ctx, a, hub, cfg, os.Stdout, true)
}

// mouseOff switches off every pointer-tracking mode, since a leftover one corrupts the next program
const mouseOff = "\x1b[?9l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?1015l\x1b[?1005l"

// keysPlain undoes the keyboard protocols v2 asks for, which left on change how a shell reads keys
var keysPlain = ansi.ResetModifyOtherKeys + ansi.PopKittyKeyboard(1)

// writeModesOff runs last whatever happened first, on the terminal the screen drew on
func writeModesOff(terminal io.Writer) {
	_, _ = io.WriteString(terminal, mouseOff+keysPlain)
}

// run is Run with the program options a test needs: a fake terminal, named on the same writer
func run(ctx context.Context, a *agent.Agent, hub *Hub, cfg config.Config, terminal io.Writer, altScreen bool, opts ...tea.ProgramOption) error {
	// before anything that can return: no path out may leave the terminal tracking the pointer or the keys
	defer writeModesOff(terminal)
	// and nothing the last program left, a killed one included, is allowed to survive into this one
	writeModesOff(terminal)
	// naming the tab is decoration: a terminal that refuses it still gets the session
	_, _ = io.WriteString(terminal, titleSequence(a.Root())) // the tab says which project this is, before the first frame
	m := newModel(ctx, a, hub, cfg)
	m.altScreen = altScreen
	program := tea.NewProgram(m, opts...)
	// everything a run reports reaches the screen through the hub, set before the program starts
	hub.wire(program.Send)
	defer hub.close()
	_, err := program.Run()
	return err
}
