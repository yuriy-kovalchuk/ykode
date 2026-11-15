// Package tui is the full-screen front end: a scrolling transcript, an input box and a
// status bar. It displays what the agent reports and answers the questions it asks.
package tui

import (
	"context"
	"io"
	"os"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

// Hub is the agent's sink while the TUI is up: events go to the screen, and a question
// waits for the answer the user types. A *Hub is handed to agent.New before the program
// exists, so it must be safe to construct first and wire up afterwards.
//
// What a run reports is queued here and the screen is only told that something waits. The run never
// stands still while a frame is drawn - a stream that waited for the screen would report the screen's
// speed as the server's - and tokens that arrive faster than frames reach the screen joined, as one
// piece of text rather than one message each.
type Hub struct {
	done chan struct{} // closed when the screen is gone: nobody is left to answer a question

	mu      sync.Mutex
	send    func(tea.Msg) // nil until Run starts the program
	waiting []tea.Msg     // what the run reported since the screen last looked, oldest first
	told    bool          // the screen has been told to look and has not yet
}

func NewHub() *Hub { return &Hub{done: make(chan struct{})} }

// wire points the hub at the program that draws. It is set once, before any run starts, and read by
// the run's goroutine for every event after that, so both go through the lock.
func (h *Hub) wire(send func(tea.Msg)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.send = send
}

// close says the screen is gone. A run still waiting on a question gets no for an answer and ends,
// instead of holding a command or an edit open for a screen that will never draw it.
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

// Ask forwards the model's own question and waits for the answer in words. Nothing typed, a run
// cancelled while it waited, and a screen that left all answer nothing.
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

// post queues a message for the screen; false means nothing is drawing yet. It returns at once: the
// screen is woken from a goroutine of its own, one at a time however much is queued behind it.
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

// joined adds a message to the queue, folding it into one already there when the two say one thing:
// text into the text before it, a piece of a call into the piece before it, and the server's speeds
// into the speeds already waiting, since only the newest of those is ever shown. The speeds may wait
// anywhere in the queue - they move no text - so they never stand between two pieces of an answer.
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

// newerSpeeds lays a later report over an earlier one. A report says nothing about a speed by leaving
// it at zero, so the earlier figure stands until a later one replaces it.
func newerSpeeds(before, next agent.Stats) agent.Stats {
	if next.DecodeTokSec > 0 {
		before.DecodeTokSec, before.Estimated, before.OutputTokens = next.DecodeTokSec, next.Estimated, next.OutputTokens
	}
	if next.PrefillTokSec > 0 {
		before.PrefillTokSec = next.PrefillTokSec
	}
	return before
}

// Run draws the session until the user quits or ctx ends. The screen takes the mouse only when
// asked: holding it gives the wheel to the transcript but leaves the terminal no way to select
// text, and ctrl+t hands it over or back at any point.
func Run(ctx context.Context, a *agent.Agent, hub *Hub, cfg config.Config) error {
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if cfg.Mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	return run(ctx, a, hub, cfg, os.Stdout, opts...)
}

// run is Run with the program options a test needs: a fake terminal instead of a real one. The
// terminal is named on the same writer the program draws to, so a test sees the tab being named too.
func run(ctx context.Context, a *agent.Agent, hub *Hub, cfg config.Config, terminal io.Writer, opts ...tea.ProgramOption) error {
	// Naming the tab is decoration: a terminal that will not take it still gets the session, and the
	// program writes its own errors to the same place and reports them through Run.
	_, _ = io.WriteString(terminal, titleSequence(a.Root())) // the tab says which project this is, before the first frame
	m := newModel(ctx, a, hub, cfg)
	program := tea.NewProgram(m, opts...)
	// everything a run reports reaches the screen through the hub: one wire, set before the
	// program starts, so a finished turn can always report itself
	hub.wire(program.Send)
	defer hub.close()
	_, err := program.Run()
	return err
}
