package app

import (
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

// only the screen has someone at the keyboard, so only it is offered ask_user: a -p run would stall
// on a question nobody can answer.
func TestOnlyTheScreenIsAsked(t *testing.T) {
	offered := func(sink agent.Sink) bool {
		_, canAnswer := sink.(agent.Asker)
		return canAnswer
	}
	if !offered(NewFrontEnd(config.Config{TUI: true}).View) {
		t.Error("the full-screen session was not offered the question")
	}
	if offered(NewFrontEnd(config.Config{Prompt: "task"}).View) {
		t.Error("a -p run was offered a question nobody can answer")
	}
}
