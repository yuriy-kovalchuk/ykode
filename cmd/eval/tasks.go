package main

import (
	"regexp"
)

// task is one scripted job: a fixture repo, a prompt, and what counts as a good outcome.
type task struct {
	name     string
	prompt   string
	followUp string // a second request in the same session, for tasks about what carries over
	fixture  map[string]string
	outside  map[string]string // files one level above the project, meant to be unreachable
	maxTurns int
	ctx      int // window to pretend the server has, to put a run under context pressure
	check    func(project string, ran *transcript, askErr error) (bool, string)
}

// wholeNumbers finds every number in an answer, so "17" or "7 or 8" is not read as 7.
var wholeNumbers = regexp.MustCompile(`\b\d+\b`)

func tasks() []task {
	return append(append(basicTasks(), pressureTasks()...), realisticTasks()...)
}
