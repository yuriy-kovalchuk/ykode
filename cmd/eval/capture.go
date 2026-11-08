package main

import (
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// transcript is what one run left behind.
type transcript struct {
	tools    []string
	args     []string // what each call was asked, in the same order as tools
	failures []string
	results  []string // what each tool handed back, one-lined
	notice   []string
	answer   string // text of the last turn that answered in prose
}

// recorder is the eval's Sink: it records the stream and answers nothing outside the defaults.
type recorder struct {
	transcript
	events       []agent.Event
	answerByTurn map[int]string
}

func newRecorder() *recorder {
	return &recorder{answerByTurn: map[int]string{}}
}

func (r *recorder) Emit(ev agent.Event) agent.Decision {
	r.events = append(r.events, ev)
	switch ev.Kind {
	case agent.Answer:
		r.answerByTurn[ev.Turn] += ev.Text
	case agent.ToolStart:
		r.tools, r.args = append(r.tools, ev.Tool), append(r.args, ev.Args)
	case agent.ToolFail:
		r.failures = append(r.failures, ev.Text)
	case agent.ToolEnd:
		r.results = append(r.results, ev.Text)
	case agent.Notice:
		r.notice = append(r.notice, ev.Text)
	}
	return agent.Refuse
}

// lastAnswer is the prose of the highest turn that answered, which is the answer to the task.
func (r *recorder) lastAnswer() string {
	best, text := 0, ""
	for turn, body := range r.answerByTurn {
		if turn > best {
			best, text = turn, body
		}
	}
	r.answer = text
	return text
}

// flat squashes a multi-line result so one transcript line stays scannable.
func flat(s string, maxChars int) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
	if len(s) > maxChars {
		return s[:maxChars] + "…"
	}
	return s
}
