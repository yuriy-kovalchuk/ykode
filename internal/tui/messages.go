package tui

import (
	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// Messages the screen receives from the run it started.
type eventMsg struct{ ev agent.Event }

type confirmMsg struct {
	ev    agent.Event
	reply chan agent.Decision
}

type runDoneMsg struct{ err error }

type compactDoneMsg struct{ freed int } // the summary the model wrote for /compact

// question is a confirmation waiting for an answer; the run is blocked until it gets one.
type question struct {
	text   string // the call in words, for a front end that cannot draw it
	tool   string
	args   string
	target []string
	reply  chan agent.Decision
}
