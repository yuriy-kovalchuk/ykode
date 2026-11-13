package app

import (
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

func TestTheScreenIsTheOnlyReaderOfStdin(t *testing.T) {
	if !readsStdinLineByLine(config.Config{}) {
		t.Error("the line prompt should read stdin")
	}
	if readsStdinLineByLine(config.Config{TUI: true}) {
		t.Error("the full-screen session would get a line reader next to it, eating the typed prompt")
	}
}

// who can be asked: the line prompt and the screen have someone at the keyboard, a run writing JSON
// lines does not, so that one is never offered a question it cannot answer without breaking its own
// stream. A tool offered but unanswerable would only stall the run.
func TestOnlyAFrontEndWithSomeoneWatchingIsAsked(t *testing.T) {
	offered := func(sink agent.Sink) bool {
		_, canAnswer := sink.(agent.Asker)
		return canAnswer
	}
	if !offered(NewFrontEnd(config.Config{}).View) {
		t.Error("the line prompt was not offered the question")
	}
	if !offered(NewFrontEnd(config.Config{TUI: true}).View) {
		t.Error("the full-screen session was not offered the question")
	}
	if offered(NewFrontEnd(config.Config{JSON: true}).View) {
		t.Error("a run writing JSON lines was offered a question nobody can answer")
	}
}
