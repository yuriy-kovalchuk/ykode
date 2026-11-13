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

// askMsg is the model's own question, waiting for an answer in words.
type askMsg struct {
	ev    agent.Event
	reply chan string // empty says the person has no answer
}

type runDoneMsg struct{ err error }

type compactDoneMsg struct{ freed int } // the summary the model wrote for /compact

// branchTickMsg is the screen asking again whether the project changed branch.
type branchTickMsg struct{}

// question is a confirmation waiting for an answer; the run is blocked until it gets one.
type question struct {
	text    string // the call in words, for a front end that cannot draw it
	tool    string
	args    string
	target  []string
	options []string    // the model's question: the choices it would act on
	words   chan string // where the answer to a question goes; nil for a call to allow or refuse
	reply   chan agent.Decision
}
