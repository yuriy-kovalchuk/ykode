package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func planned(t *testing.T, p *Plan, steps string) string {
	t.Helper()
	out, err := p.Run(context.Background(), json.RawMessage(steps))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// the model sends the whole list each time, so the answer is the plan as it now stands
func TestAPlanCallReplacesTheListAndReadsBack(t *testing.T) {
	p := NewPlan()
	planned(t, p, `{"steps":[{"text":"read the loop","status":"done"},{"text":"write the panel","status":"active"},{"text":"test it"}]}`)

	got := planned(t, p, `{"steps":[{"text":"write the panel","status":"done"},{"text":"test it","status":"active"}]}`)
	want := "plan 1/2\n[x] write the panel\n[*] test it\n(call plan again when the active step is finished, before starting the next)"
	if got != want {
		t.Errorf("answered:\n%s\nwant:\n%s", got, want)
	}
	steps := p.Steps()
	if len(steps) != 2 || steps[0].Status != PlanDone || steps[1].Status != PlanActive {
		t.Errorf("Steps() = %+v", steps)
	}
}

// a made-up status is a step not started, and an empty step is not a step
func TestAPlanReadsWhatItWasNotGiven(t *testing.T) {
	p := NewPlan()
	planned(t, p, `{"steps":[{"text":"one","status":"nearly-there"},{"text":"  ","status":"done"},{"text":"two","status":"failed"}]}`)

	steps := p.Steps()
	if len(steps) != 2 {
		t.Fatalf("%d steps, want the blank one gone: %+v", len(steps), steps)
	}
	if steps[0].Status != PlanPending {
		t.Errorf("an unknown status read as %q, want pending", steps[0].Status)
	}
	if got := p.Report(); !strings.Contains(got, "[!] two") || !strings.Contains(got, "plan 0/2") {
		t.Errorf("Report() = %q", got)
	}
}

// a plan is only worth sending when it holds steps
func TestAnEmptyPlanIsRefused(t *testing.T) {
	p := NewPlan()
	for _, body := range []string{`{"steps":[]}`, `{"steps":[{"text":"   "}]}`, `{}`} {
		if _, err := p.Run(context.Background(), json.RawMessage(body)); err == nil || !strings.Contains(err.Error(), "plan") {
			t.Errorf("%s was accepted: %v", body, err)
		}
	}
	if got := p.Report(); got != "plan: nothing planned" {
		t.Errorf("a run with no plan reports %q", got)
	}
}

// a list longer than a plan is worth keeping is cut, so the panel never takes the screen
func TestALongPlanIsCutToAScreen(t *testing.T) {
	var parts []string
	for range maxPlanSteps + 5 {
		parts = append(parts, `{"text":"step"}`)
	}
	p := NewPlan()
	planned(t, p, `{"steps":[`+strings.Join(parts, ",")+`]}`)
	if got := len(p.Steps()); got != maxPlanSteps {
		t.Errorf("%d steps kept, want the list cut to %d", got, maxPlanSteps)
	}
}

// a failed step is an outcome: a plan that ended badly is settled, one with work left is not
func TestAPlanIsSettledWhenEveryStepHasAnOutcome(t *testing.T) {
	for _, c := range []struct {
		what     string
		statuses []string
		settled  bool
		left     int // steps the plan still holds once a settled one is dropped
	}{
		{"every step done", []string{PlanDone, PlanDone}, true, 0},
		{"one step failed", []string{PlanDone, PlanFailed}, true, 0},
		{"one step still going", []string{PlanDone, PlanActive}, false, 2},
		{"one step not started", []string{PlanDone, PlanPending}, false, 2},
		{"no plan at all", nil, false, 0},
	} {
		p := NewPlan()
		if c.statuses != nil {
			var parts []string
			for i, s := range c.statuses {
				parts = append(parts, fmt.Sprintf(`{"text":"step %d","status":%q}`, i+1, s))
			}
			planned(t, p, `{"steps":[`+strings.Join(parts, ",")+`]}`)
		}
		if got := p.Settled(); got != c.settled {
			t.Errorf("%s: Settled() = %v, want %v", c.what, got, c.settled)
		}
		p.DropIfSettled()
		if kept := len(p.Steps()); kept != c.left {
			t.Errorf("%s: %d steps left after DropIfSettled, want %d", c.what, kept, c.left)
		}
	}
}

func TestAPlanIsKeptByTheToolThatWritesIt(t *testing.T) {
	p := NewPlan()
	planned(t, p, `{"steps":[{"text":"one","status":"done"},{"text":"two"}]}`)
	if got := p.Report(); !strings.HasPrefix(got, "plan 1/2") {
		t.Errorf("Report() = %q", got)
	}
	p.Clear()
	if len(p.Steps()) != 0 || p.Report() != "plan: nothing planned" {
		t.Errorf("after Clear: %q", p.Report())
	}
}

// the screen reads the plan while the run may still be writing it, so the two must not race
func TestThePlanIsSafeToReadWhileItIsWritten(t *testing.T) {
	p := NewPlan()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = p.Run(context.Background(), json.RawMessage(`{"steps":[{"text":"step","status":"active"}]}`))
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				_ = p.Report()
			}
		}()
	}
	wg.Wait()
}

// the statuses other agents use are read as the ones the plan keeps
func TestPlanReadsTheStatusesOtherAgentsUse(t *testing.T) {
	for in, want := range map[string]string{
		"active": PlanActive, "in_progress": PlanActive, "In-Progress": PlanActive,
		"done": PlanDone, "completed": PlanDone, "finished": PlanDone,
		"failed": PlanFailed, "blocked": PlanFailed,
		"pending": PlanPending, "": PlanPending, "whatever": PlanPending,
	} {
		if got := statusOf(in); got != want {
			t.Errorf("statusOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// a plan written as a call, and as the report a fold carries, comes back as the same steps
func TestAPlanCanBeReadBackFromItsCallAndItsReport(t *testing.T) {
	p := NewPlan()
	args := json.RawMessage(`{"steps":[{"text":"read","status":"done"},{"text":"build","status":"active"},{"text":"test"}]}`)
	if _, err := p.Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	want := p.Steps()
	if got := StepsFrom(args); !reflect.DeepEqual(got, want) {
		t.Errorf("from the call: %+v, want %+v", got, want)
	}
	if got := StepsFromReport(p.Report()); !reflect.DeepEqual(got, want) {
		t.Errorf("from the report: %+v, want %+v", got, want)
	}
	q := NewPlan()
	q.Restore(nil)
	if len(q.Steps()) != 0 {
		t.Error("restoring nothing left something")
	}
	q.Restore(want)
	if !reflect.DeepEqual(q.Steps(), want) {
		t.Error("a restored plan differs")
	}
}

// a plan with work left says to return to it, and steps left out are said to be left out
func TestAPlanSaysWhatTheModelShouldKnow(t *testing.T) {
	p := NewPlan()
	if got := planned(t, p, `{"steps":[{"text":"one","status":"done"},{"text":"two","status":"failed"}]}`); strings.Contains(got, "call plan again") {
		t.Errorf("a plan that is over asks to be updated: %q", got)
	}
	var steps []string
	for i := 0; i < maxPlanSteps+3; i++ {
		steps = append(steps, fmt.Sprintf(`{"text":"step %d"}`, i))
	}
	got := planned(t, p, `{"steps":[`+strings.Join(steps, ",")+`]}`)
	if !strings.Contains(got, "the 3 after those were left out") || !strings.Contains(got, "call plan again") {
		t.Errorf("fifteen steps answered %q", got)
	}
	if len(p.Steps()) != maxPlanSteps {
		t.Errorf("the plan holds %d steps", len(p.Steps()))
	}
}
