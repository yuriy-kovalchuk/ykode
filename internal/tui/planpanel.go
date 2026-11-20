package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// the plan is pinned under the session where it cannot scroll away, one row per step, with a bar
const (
	planLead     = "  "
	planCellDone = "▰"
	planCellRest = "▱"
	planMinRows  = 2  // the fewest steps worth showing; fewer than this and the panel folds to its heading
	planChrome   = 14 // rows the session keeps for itself: the prompt, the report, the rule and some transcript
)

// planRows is the panel as rows: empty with no plan, one row when folded, short, or finished
func (m *model) planRows() []string {
	steps := m.agent.Plan()
	if len(steps) == 0 {
		return nil
	}
	done, at := planProgress(steps)
	finished := tools.Settled(steps)
	head := planLead + styleBold.Render("plan") + " " + planBar(steps, m.running) + " " +
		styleDim.Render(fmt.Sprintf("%d/%d", done, len(steps)))
	if finished {
		head = planLead + planVerdict(steps) + " " + planBar(steps, false) + " " +
			styleDim.Render(planTally(done, len(steps)))
	}
	room := m.height - planChrome
	if m.planFolded || (finished && !m.running) || room < planMinRows+1 {
		if at >= 0 && !finished {
			head += styleDim.Render(" · ") + cutToWidth(steps[at].Text, max(m.width-lipgloss.Width(head)-4, 10))
		}
		return []string{head}
	}
	avail := room - 1 // one row of the room is the heading
	blocks := make([][]string, len(steps))
	for i, s := range steps {
		blocks[i] = m.planStep(s)
	}
	first, last := planFit(blocks, at, avail)
	rows := []string{head}
	if first > 0 {
		rows = append(rows, styleDim.Render(fmt.Sprintf("%s… %d done above", planLead, first)))
	}
	shown := avail - min(first, 1) - min(len(steps)-last, 1)
	for i := first; i < last; i++ {
		block := blocks[i]
		if len(block) > shown {
			block = block[:max(shown, 1)] // one step taller than the whole room keeps the rows that fit
		}
		rows = append(rows, block...)
		shown -= len(block)
	}
	if last < len(steps) {
		rows = append(rows, styleDim.Render(fmt.Sprintf("%s… %d more", planLead, len(steps)-last)))
	}
	return rows
}

// planVerdict is a finished plan's word: a tick when all is done, failure in its usual colour
func planVerdict(steps []tools.PlanStep) string {
	for _, s := range steps {
		if s.Status == tools.PlanFailed {
			return styleBad.Render("plan ✗")
		}
	}
	return styleOK.Render("plan ✓")
}

// planTally counts the steps done; "done" is said only when all of them are
func planTally(done, total int) string {
	if done == total {
		return fmt.Sprintf("%d/%d done", done, total)
	}
	return fmt.Sprintf("%d/%d", done, total)
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

// planBar is one cell per step: green done, accent active, red failed, hollow the rest
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

// planStep is one step's rows: its mark then its words, wrapped under the first word
func (m *model) planStep(s tools.PlanStep) []string {
	mark, dress := styleDim.Render("○ "), func(t string) string { return styleDim.Render(t) }
	switch s.Status {
	case tools.PlanDone:
		mark = styleOK.Render("✓ ")
	case tools.PlanFailed:
		mark, dress = styleBad.Render("✗ "), func(t string) string { return t }
	case tools.PlanActive:
		mark, dress = styleAccent.Render("● "), func(t string) string { return styleBold.Render(t) }
		if m.running {
			mark = m.spin.View() + " "
		}
	}
	indent := planLead + "  " // the continuation rows start where the words do
	lines := wrap(s.Text, max(m.width-lipgloss.Width(indent)-2, 10))
	rows := make([]string, len(lines))
	for i, line := range lines {
		lead := indent
		if i == 0 {
			lead = planLead + mark
		}
		rows[i] = lead + dress(line)
	}
	return rows
}

// planFit drops steps, far end first and never the active one, until their rows fit
func planFit(blocks [][]string, active, room int) (first, last int) {
	first, last = planWindow(len(blocks), active, room)
	height := func() int {
		h := min(first, 1) + min(len(blocks)-last, 1)
		for _, b := range blocks[first:last] {
			h += len(b)
		}
		return h
	}
	for last-first > 1 && height() > room {
		if active < 0 || last-1 > active {
			last--
		} else {
			first++
		}
	}
	return first, last
}

// planWindow shows the active step with its neighbours, slid back at the end, else from the top
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
