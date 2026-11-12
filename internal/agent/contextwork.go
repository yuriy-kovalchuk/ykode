package agent

import (
	"strconv"
	"sync"
)

// contextWork is what the last operation that made room in the context window did, in the few words a
// front end can put beside its working prompt. The session itself gets the whole sentence; a reader
// watching the clock only wants to know why this round-trip is taking longer.
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
	roomShortened = "shortened old output"
	roomFolding   = "summarising older turns" // set before the fold's request, which is what the clock waits on
)

func roomFolded(turns int) string { return "folded " + counted(turns, "older turn", "older turns") }
func roomTrimmed(results int) string {
	return "trimmed " + counted(results, "tool result", "tool results")
}
func roomDropped(turns int) string { return "dropped " + counted(turns, "older turn", "older turns") }

// counted is a count and the thing it counts, as a footer reads them.
func counted(n int, one, several string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + several
}

// RoomMade is what this ask has had to do to fit the conversation into the window - "shortened old
// output", "folded 12 older turns" - and "" when nothing has had to.
func (a *Agent) RoomMade() string { return a.work.since() }
