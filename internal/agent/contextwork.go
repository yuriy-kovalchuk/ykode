package agent

import (
	"strconv"
	"sync"
)

// contextWork is what the last operation that made room did, in words for the working prompt
type contextWork struct {
	mu    sync.Mutex
	label string // what made room last; empty until the next ask
}

// made records that room was just made by whatever the label names.
func (w *contextWork) made(label string) {
	w.mu.Lock()
	w.label = label
	w.mu.Unlock()
}

// started is a new ask: nothing has had to make room in it yet.
func (w *contextWork) started() { w.made("") }

// since is what has been done in this ask so far, "" when nothing has had to be.
func (w *contextWork) since() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.label
}

// The labels each operation leaves behind, in the order they get their turn.
const (
	roomFolding = "summarising older turns" // set before the fold's request, which is what the clock waits on
)

func roomFolded(turns int) string { return "folded " + counted(turns, "older turn", "older turns") }
func roomDropped(messages int) string {
	return "dropped " + counted(messages, "older message", "older messages")
}

// counted is a count and the thing it counts, as a footer reads them.
func counted(n int, one, several string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + several
}

// RoomMade is what this ask had to do to fit the window, or "" when nothing had to
func (a *Agent) RoomMade() string { return a.work.since() }
