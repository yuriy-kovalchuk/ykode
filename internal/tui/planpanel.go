package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// The plan is pinned under the session, above the prompt, where it cannot scroll away: the steps the
// model wrote for a long task, one row each, with a bar of one cell per step on its heading row.
const (
	planLead     = "  "
	planCellDone = "▰"
	planCellRest = "▱"
	planMinRows  = 2  // the fewest steps worth showing; fewer than this and the panel folds to its heading
	planChrome   = 14 // rows the session keeps for itself: the prompt, the report, the rule and some transcript
)

// planRows is the panel as rows of the bottom area. It is empty when the model wrote no plan, and one
// row when the panel is folded, when the terminal is too short for steps, or when every step is done and
// nothing is running: a finished plan is a record, not a thing to keep reading.
func (m *model) planRows() []string {
	steps := m.agent.Plan()
	if len(steps) == 0 {
		return nil
	}
	done, at := planProgress(steps)
	head := planLead + styleBold.Render("plan") + " " + planBar(steps, m.running) + " " +
		styleDim.Render(fmt.Sprintf("%d/%d", done, len(steps)))
	finished := done == len(steps)
	if finished {
		head = planLead + styleOK.Render("plan ✓") + " " + planBar(steps, false) + " " +
			styleDim.Render(fmt.Sprintf("%d/%d done", done, len(steps)))
	}
	room := m.height - planChrome
	if m.planFolded || (finished && !m.running) || room < planMinRows+1 {
		if at >= 0 && !finished {
			head += styleDim.Render(" · ") + cutToWidth(steps[at].Text, max(m.width-lipgloss.Width(head)-4, 10))
		}
		return []string{head}
	}
	avail := room - 1 // one row of the room is the heading
	if len(steps) > avail {
		avail = max(avail-2, 1) // and a window that cuts steps says so above and below, a row each
	}
	first, last := planWindow(len(steps), at, avail)
	rows := []string{head}
	if first > 0 {
		rows = append(rows, styleDim.Render(fmt.Sprintf("%s… %d done above", planLead, first)))
	}
	for i := first; i < last; i++ {
		rows = append(rows, m.planStep(steps[i]))
	}
	if last < len(steps) {
		rows = append(rows, styleDim.Render(fmt.Sprintf("%s… %d more", planLead, len(steps)-last)))
	}
	return rows
}

// planProgress counts the steps done and finds the one being worked on, or -1 when none is.
func planProgress(steps []tools.PlanStep) (done, active int) {
	active = -1
	for i, s := range steps {
		switch s.Status {
		case tools.PlanDone:
			done++
		case tools.PlanActive:
			if active < 0 {
				active = i
			}
		}
	}
	return done, active
}

// planBar is one cell per step: filled and green for a step done, filled in the accent colour for the one
// being worked on (and in the working colour while a turn runs), red for one that failed, hollow for the
// rest. It reads at a glance where the task stands without reading a step.
func planBar(steps []tools.PlanStep, running bool) string {
	var b strings.Builder
	for _, s := range steps {
		switch s.Status {
		case tools.PlanDone:
			b.WriteString(styleOK.Render(planCellDone))
		case tools.PlanActive:
			if running {
				b.WriteString(styleWorking.Render(planCellDone))
			} else {
				b.WriteString(styleAccent.Render(planCellDone))
			}
		case tools.PlanFailed:
			b.WriteString(styleBad.Render(planCellDone))
		default:
			b.WriteString(styleDim.Render(planCellRest))
		}
	}
	return b.String()
}

// planStep is one step's row: its mark, then its words. The step being worked on turns with the
// spinner while a turn runs, and is the only bold one.
func (m *model) planStep(s tools.PlanStep) string {
	room := max(m.width-lipgloss.Width(planLead)-4, 10)
	text := cutToWidth(s.Text, room)
	switch s.Status {
	case tools.PlanDone:
		return planLead + styleOK.Render("✓ ") + styleDim.Render(text)
	case tools.PlanFailed:
		return planLead + styleBad.Render("✗ ") + text
	case tools.PlanActive:
		mark := styleAccent.Render("● ")
		if m.running {
			mark = m.spin.View() + " "
		}
		return planLead + mark + styleBold.Render(text)
	}
	return planLead + styleDim.Render("○ "+text)
}

// planWindow picks the steps to show when they do not all fit: the active one with the steps around it,
// the window slid back when it would run past the end. With no step active it starts at the top.
func planWindow(total, active, rows int) (first, last int) {
	rows = max(rows, 1)
	if total <= rows {
		return 0, total
	}
	first = 0
	if active >= 0 {
		first = max(active-rows/3, 0) // a little of what came before, and most of what is ahead
	}
	first = min(first, total-rows)
	return first, first + rows
}
