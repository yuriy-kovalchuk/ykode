package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// a screen whose agent holds a plan, which is how the model's plan call reaches it
func planModel(t *testing.T, width, height int, steps ...tools.PlanStep) *model {
	t.Helper()
	plan := tools.NewPlan()
	a := agent.New(nil, nil, "test-model", agent.Options{Plan: plan}, nil)
	plan.Restore(steps) // after New, which starts a conversation and so clears any plan
	m := newModel(context.Background(), a, NewHub(), config.Config{})
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return m
}

func steps(statuses ...string) []tools.PlanStep {
	out := make([]tools.PlanStep, len(statuses))
	for i, s := range statuses {
		out[i] = tools.PlanStep{Text: fmt.Sprintf("step %d", i+1), Status: s}
	}
	return out
}

const (
	done, active, pending, failed = tools.PlanDone, tools.PlanActive, tools.PlanPending, tools.PlanFailed
)

// no plan, no panel
func TestNoPlanLeavesNoPanel(t *testing.T) {
	m := planModel(t, 100, 40)
	if rows := m.planRows(); rows != nil {
		t.Errorf("a screen with no plan drew %v", rows)
	}
}

// the panel is a heading with one cell per step, then one row per step, each with its mark
func TestThePlanIsAHeadingWithABarAndOneRowPerStep(t *testing.T) {
	m := planModel(t, 100, 40, steps(done, done, active, pending, failed)...)
	rows := m.planRows()
	if len(rows) != 6 {
		t.Fatalf("%d rows for five steps:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	plain := make([]string, len(rows))
	for i, r := range rows {
		plain[i] = stripCodes(r)
	}
	if !strings.Contains(plain[0], "plan ▰▰▰▱▰ 2/5") {
		t.Errorf("the heading is %q: one cell per step, filled for done, active and failed, hollow for pending", plain[0])
	}
	for i, want := range []string{"✓ step 1", "✓ step 2", "● step 3", "○ step 4", "✗ step 5"} {
		if !strings.Contains(plain[i+1], want) {
			t.Errorf("row %d is %q, want it to hold %q", i+1, plain[i+1], want)
		}
	}
}

// while a turn runs, the step being worked on turns with the spinner instead of sitting still
func TestTheActiveStepTurnsWhileATurnRuns(t *testing.T) {
	m := planModel(t, 100, 40, steps(done, active)...)
	idle := stripCodes(m.planRows()[2])
	m.running = true
	busy := stripCodes(m.planRows()[2])
	if !strings.Contains(idle, "● ") || strings.Contains(busy, "● ") {
		t.Errorf("idle %q, running %q: the running step should show the spinner's frame, not the still dot", idle, busy)
	}
}

// ctrl+o folds the panel to its heading, which names the step being worked on
func TestCtrlOFoldsThePlanToItsHeading(t *testing.T) {
	m := planModel(t, 100, 40, steps(done, active, pending)...)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	rows := m.planRows()
	if len(rows) != 1 || !strings.Contains(stripCodes(rows[0]), "step 2") || !strings.Contains(stripCodes(rows[0]), "1/3") {
		t.Fatalf("folded plan = %q", rows)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if len(m.planRows()) != 4 {
		t.Error("ctrl+o again did not unfold it")
	}
}

// a plan with every step done is a record: one row, ticked, unless a turn is still running
func TestAFinishedPlanFoldsToATickedHeading(t *testing.T) {
	m := planModel(t, 100, 40, steps(done, done, done)...)
	rows := m.planRows()
	if len(rows) != 1 || !strings.Contains(stripCodes(rows[0]), "plan ✓ ▰▰▰ 3/3 done") {
		t.Errorf("a finished plan = %q", rows)
	}
}

// a plan that ended badly folds too and says it failed instead of claiming it finished
func TestAPlanThatEndedBadlyFoldsAndSaysSo(t *testing.T) {
	m := planModel(t, 100, 40, steps(done, failed, done)...)
	rows := m.planRows()
	if len(rows) != 1 {
		t.Fatalf("a plan with a failed step holds %d rows: %q", len(rows), rows)
	}
	if plain := stripCodes(rows[0]); !strings.Contains(plain, "plan ✗") || strings.Contains(plain, "done") {
		t.Errorf("the heading reads %q, want plan ✗ and no claim of done", plain)
	}
}

// a short terminal has no room for steps, so the panel keeps to its heading
func TestAShortTerminalGetsOnlyTheHeading(t *testing.T) {
	m := planModel(t, 100, 14, steps(done, active, pending)...)
	if rows := m.planRows(); len(rows) != 1 {
		t.Errorf("%d rows on a 14 row terminal", len(rows))
	}
}

// more steps than rows: the active step stays in view, and what is cut says how much
func TestALongPlanKeepsTheActiveStepInView(t *testing.T) {
	m := planModel(t, 100, 24, steps(done, done, done, done, done, active, pending, pending, pending, pending, pending, pending)...)
	rows := m.planRows()
	text := stripCodes(strings.Join(rows, "\n"))
	if !strings.Contains(text, "● step 6") {
		t.Errorf("the active step is not on the screen:\n%s", text)
	}
	if !strings.Contains(text, "… ") || !strings.Contains(text, "more") {
		t.Errorf("the rows left out are not counted:\n%s", text)
	}
	if len(rows) > 24-planChrome+1 {
		t.Errorf("%d rows for a room of %d", len(rows), 24-planChrome)
	}
	first, last := planWindow(12, 5, 4)
	if first > 5 || last <= 5 || last-first != 4 {
		t.Errorf("window %d..%d does not hold step 5", first, last)
	}
	if first, last := planWindow(3, -1, 8); first != 0 || last != 3 {
		t.Errorf("a plan that fits is windowed %d..%d", first, last)
	}
}

// the panel sits in the bottom area between the rule and the prompt, and never runs off the screen
func TestThePanelSitsAbovePromptAndWithinTheScreen(t *testing.T) {
	m := planModel(t, 60, 30, steps(done, active, pending)...)
	area := stripCodes(m.bottomArea())
	if p, q := strings.Index(area, "plan"), strings.Index(area, promptMark); p < 0 || q < p {
		t.Errorf("the panel is not above the prompt:\n%s", area)
	}
	for _, row := range strings.Split(area, "\n") {
		if w := len([]rune(row)); w > 60 {
			t.Errorf("a row is %d columns on a 60 column screen: %q", w, row)
		}
	}
}

// a plan call finishing redraws the screen, so the panel and the room left for the session agree
func TestAPlanCallGrowsThePanelAndShrinksTheSession(t *testing.T) {
	plan := tools.NewPlan()
	a := agent.New(nil, nil, "test-model", agent.Options{Plan: plan}, nil)
	m := newModel(context.Background(), a, NewHub(), config.Config{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	before := m.view.height
	args := `{"steps":[{"text":"a","status":"done"},{"text":"b","status":"active"},{"text":"c"}]}`
	if _, err := plan.Run(context.Background(), []byte(args)); err != nil { // the tool writes the list...
		t.Fatal(err)
	}
	m.handle(agent.Event{Kind: agent.ToolEnd, Tool: "plan", Args: args, Text: plan.Report()}) // ...and the event reaches the screen
	if m.view.height != before-4 {
		t.Errorf("the transcript has %d rows, was %d: a heading and three steps should have taken four", m.view.height, before)
	}
	if view := stripCodes(m.View()); !strings.Contains(view, "● b") || !strings.Contains(view, "plan ▰▰▱ 1/3") {
		t.Errorf("the panel is not on the screen:\n%s", view)
	}
}

// a plan call is one row that says how far the plan got, not the word plan twice
func TestAPlanCallIsOneRowSayingHowFarItGot(t *testing.T) {
	var log transcript
	args := `{"steps":[{"text":"read","status":"done"},{"text":"build the panel","status":"active"},{"text":"test"}]}`
	log.toolStart("plan", args)
	log.toolEnd("plan", args, "plan 1/3\n[x] read\n[*] build the panel\n[ ] test")
	row := squeezed(stripCodes(log.render(100)))
	if !strings.Contains(row, "plan 1/3 steps · build the panel") || strings.Contains(row, "plan plan") || strings.Count(row, "\n") != 0 {
		t.Errorf("the plan row is %q", row)
	}
}

// a step longer than the screen goes on to the next row, under its first word, instead of being cut
func TestALongStepWrapsInsteadOfBeingCut(t *testing.T) {
	long := "session: bump format to 2, attachments round-trip through the log -> verify: round-trip test + the old format still loads"
	m := planModel(t, 60, 40, tools.PlanStep{Text: "short", Status: done}, tools.PlanStep{Text: long, Status: active})
	rows := m.planRows()
	text := stripCodes(strings.Join(rows, "\n"))
	if strings.Contains(text, "…") {
		t.Errorf("the step was cut:\n%s", text)
	}
	if len(rows) < 4 {
		t.Fatalf("%d rows: the long step did not wrap:\n%s", len(rows), text)
	}
	if !strings.Contains(strings.Join(strings.Fields(text), " "), "the old format still loads") {
		t.Errorf("the end of the step is missing:\n%s", text)
	}
	for _, row := range rows {
		if w := len([]rune(stripCodes(row))); w > 60 {
			t.Errorf("a row is %d columns on a 60 column screen: %q", w, stripCodes(row))
		}
	}
	if cont := stripCodes(rows[3]); !strings.HasPrefix(cont, planLead+"  ") {
		t.Errorf("the continuation row %q does not start under the step's words", cont)
	}
}

// wrapped steps still fit the room: steps give way, and what was left out is counted
func TestWrappedStepsStillFitTheRoom(t *testing.T) {
	var many []tools.PlanStep
	for i := range 8 {
		status := pending
		if i == 2 {
			status = active
		}
		many = append(many, tools.PlanStep{Text: fmt.Sprintf("step %d %s", i+1, strings.Repeat("word ", 20)), Status: status})
	}
	m := planModel(t, 60, 24, many...)
	rows := m.planRows()
	if len(rows) > 24-planChrome {
		t.Errorf("%d rows for a room of %d", len(rows), 24-planChrome)
	}
	text := stripCodes(strings.Join(rows, "\n"))
	if !strings.Contains(text, "step 3") || !strings.Contains(text, "more") {
		t.Errorf("the active step or the count of the rest is missing:\n%s", text)
	}
}
