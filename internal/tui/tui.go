// Package tui is the full-screen front end: a scrolling transcript, an input box and a
// status bar. It displays what the agent reports and answers the questions it asks.
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// Hub is the agent's sink while the TUI is up: events go to the screen, and a question
// waits for the answer the user types. A *Hub is handed to agent.New before the program
// exists, so it must be safe to construct first and wire up afterwards.
type Hub struct {
	send func(tea.Msg) // nil until Run starts the program
}

func NewHub() *Hub { return &Hub{} }

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
	return <-reply
}

// post hands a message to the screen; false means nothing is drawing yet.
func (h *Hub) post(msg tea.Msg) bool {
	if h.send == nil {
		return false
	}
	h.send(msg)
	return true
}

// Run draws the session until the user quits or ctx ends.
func Run(ctx context.Context, a *agent.Agent, hub *Hub) error {
	return run(ctx, a, hub, tea.WithAltScreen())
}

// run is Run with the program options a test needs: a fake terminal instead of a real one.
func run(ctx context.Context, a *agent.Agent, hub *Hub, opts ...tea.ProgramOption) error {
	m := newModel(ctx, a, hub)
	program := tea.NewProgram(m, opts...)
	// everything a run reports reaches the screen through the hub: one wire, set before the
	// program starts, so a finished turn can always report itself
	hub.send = program.Send
	_, err := program.Run()
	return err
}
