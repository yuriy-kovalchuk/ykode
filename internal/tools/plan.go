package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Plan is a long task broken into steps. The model writes the list and updates it as it works; the
// screen pins it where it cannot scroll away. Each call replaces the list whole, so the model never
// has to name or merge steps and what the screen reads is never half an edit.
type Plan struct {
	mu    sync.Mutex
	steps []PlanStep
}

// PlanStep is one step and how far the run got with it.
type PlanStep struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

// What a step can be. A status outside these reads as pending: the schema lists them, so a server
// that ignores the list loses nothing.
const (
	PlanPending = "pending"
	PlanActive  = "active"
	PlanDone    = "done"
	PlanFailed  = "failed"
)

const (
	maxPlanSteps  = 12  // steps one plan holds; longer work is a task the model should fold
	planStepChars = 100 // what one step says
)

func NewPlan() *Plan { return &Plan{} }

func (*Plan) Name() string { return "plan" }

func (*Plan) Description() string {
	return "Keep a checklist for a long task: send every step each call, with its status (pending, active for the one you are on, done, failed). Skip it for work that needs no plan."
}

func (*Plan) NeedsConfirm() bool { return false }

func (*Plan) Targets(json.RawMessage) ([]Target, error) { return nil, nil }

func (*Plan) Schema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
	  "type": "object",
	  "properties": {
	    "steps": {
	      "type": "array",
	      "maxItems": %d,
	      "items": {
	        "type": "object",
	        "properties": {
	          "text": {"type": "string"},
	          "status": {"type": "string", "enum": ["pending", "active", "done", "failed"]}
	        },
	        "required": ["text"]
	      }
	    }
	  },
	  "required": ["steps"]
	}`, maxPlanSteps))
}

type planInput struct {
	Steps []PlanStep `json:"steps"`
}

func (p *Plan) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in planInput
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	steps := cleanSteps(in.Steps)
	if len(steps) == 0 {
		return "", errors.New("plan: no step to keep; send the list again with its steps written out")
	}
	p.set(steps)
	return p.Report(), nil
}

// cleanSteps is a list as the plan keeps it: each step on one line, a step with nothing in it dropped
// (it would be a row of nothing), the statuses read, and no more steps than a screen can hold.
func cleanSteps(in []PlanStep) []PlanStep {
	steps := make([]PlanStep, 0, min(len(in), maxPlanSteps))
	for _, s := range in {
		text := oneLine(s.Text, planStepChars)
		if text == "" {
			continue
		}
		if len(steps) == maxPlanSteps {
			break
		}
		steps = append(steps, PlanStep{Text: text, Status: statusOf(s.Status)})
	}
	return steps
}

// StepsFrom reads the steps out of a plan call's arguments, for a run that comes back to a saved
// conversation and finds the plan in what the model wrote.
func StepsFrom(args json.RawMessage) []PlanStep {
	var in planInput
	if json.Unmarshal(args, &in) != nil {
		return nil
	}
	return cleanSteps(in.Steps)
}

// StepsFromReport reads a plan back out of its Report, which is how a fold carries it in the recap.
func StepsFromReport(text string) []PlanStep {
	var steps []PlanStep
	for _, line := range strings.Split(text, "\n") {
		for status, box := range map[string]string{PlanPending: "[ ] ", PlanActive: "[*] ", PlanDone: "[x] ", PlanFailed: "[!] "} {
			if rest, ok := strings.CutPrefix(line, box); ok {
				steps = append(steps, PlanStep{Text: rest, Status: status})
			}
		}
	}
	return cleanSteps(steps)
}

// Restore puts a plan back, for a resumed conversation. Nothing is put back when there is nothing.
func (p *Plan) Restore(steps []PlanStep) {
	if p != nil && len(steps) > 0 {
		p.set(steps)
	}
}

// Settled says every step has an outcome, so the plan describes work that is over. A failed step is an
// outcome too: a plan that ended badly is settled, and should stop holding rows on the screen.
func (p *Plan) Settled() bool { return Settled(p.Steps()) }

// Settled is the same question asked of steps a reader already holds.
func Settled(steps []PlanStep) bool {
	if len(steps) == 0 {
		return false
	}
	for _, s := range steps {
		if s.Status == PlanPending || s.Status == PlanActive {
			return false
		}
	}
	return true
}

// DropIfSettled forgets a plan whose every step has an outcome, and keeps one with work left: the
// steps belonged to the prompt that finished them, while a plan still being worked on belongs to the
// next prompt as much as to the last.
func (p *Plan) DropIfSettled() {
	if p.Settled() {
		p.Clear()
	}
}

// statusOf reads a status, and a step that named none or named one that is not on the list is a step
// that has not been started.
func statusOf(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case PlanActive, "in_progress", "in-progress", "doing", "current":
		return PlanActive
	case PlanDone, "completed", "complete", "finished":
		return PlanDone
	case PlanFailed, "blocked", "error":
		return PlanFailed
	}
	return PlanPending
}

// Steps is the list as a reader outside the run sees it. The run writes it while the screen reads, so
// what comes back is a copy.
func (p *Plan) Steps() []PlanStep {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]PlanStep(nil), p.steps...)
}

func (p *Plan) set(steps []PlanStep) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.steps = steps
}

// Clear forgets the plan: it belongs to the conversation /reset forgets.
func (p *Plan) Clear() {
	if p == nil {
		return
	}
	p.set(nil)
}

// Report is the plan in words, for the model to read back and for a front end that prints what a tool
// answered. The boxes are plain so they survive a log, a pipe and a terminal without colours.
func (p *Plan) Report() string {
	steps := p.Steps()
	if len(steps) == 0 {
		return "plan: nothing planned"
	}
	done := 0
	for _, s := range steps {
		if s.Status == PlanDone {
			done++
		}
	}
	b := strings.Builder{}
	fmt.Fprintf(&b, "plan %d/%d", done, len(steps))
	for _, s := range steps {
		fmt.Fprintf(&b, "\n%s %s", boxOf(s.Status), s.Text)
	}
	return b.String()
}

// boxOf is a step's state in one box: the plan as the model wrote it, read back at it.
func boxOf(status string) string {
	switch status {
	case PlanActive:
		return "[*]"
	case PlanDone:
		return "[x]"
	case PlanFailed:
		return "[!]"
	}
	return "[ ]"
}

// planShape words a plan call for a row: how far the plan got, and what it is on now. The whole list
// would not fit on a row; the screen that shows it has the room and reads the steps themselves.
func planShape(in planInput) string {
	if len(in.Steps) == 0 {
		return "no steps"
	}
	done, active := 0, ""
	for _, s := range in.Steps {
		switch statusOf(s.Status) {
		case PlanDone:
			done++
		case PlanActive:
			if active == "" {
				active = oneLine(s.Text, 40)
			}
		}
	}
	where := fmt.Sprintf("%d/%d steps", done, len(in.Steps))
	if active == "" {
		return where
	}
	return where + " · " + active
}
