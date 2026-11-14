package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// sessionReport is /ctx and /usage: what this run's window holds and what the conversation has cost the
// server, as figures a reader can see rather than a paragraph of them. It is read, not chosen from, so it
// is the plain half of the same box a list opens.
type report struct {
	title string
	facts []reportFact
	note  string
}

// reportFact is one figure: what it is called, what it is, and how full it is, which is the only thing
// worth a bar. An empty gauge means the figure has no share of anything to show.
type reportFact struct {
	name  string
	value string
	gauge string
}

// fact and gauge are the two kinds of figure.
func fact(name, value string) reportFact { return reportFact{name: name, value: value} }

func gauge(name, value string, pct int) reportFact {
	return reportFact{name: name, value: value, gauge: fullness(pct).Render(bar(pct, 24))}
}

// sessionReport reads the figures from this run. Usage is what the last turn measured; the window is the
// budget, so the two together say how much room is left before the conversation has to be folded.
func (m *model) sessionReport() *report {
	u, ctxSize := m.agent.Usage(), m.agent.CtxSize()
	facts := []reportFact{}
	switch {
	case ctxSize > 0:
		pct := u.EstTok * 100 / ctxSize
		facts = append(facts, fact("window", tokCount(ctxSize)+" tokens"))
		facts = append(facts, gauge("used for the next request", fmt.Sprintf("~%s of it, %d%%", tokCount(u.EstTok), pct), pct))
	default:
		facts = append(facts, fact("window", "unknown: set --ctx to budget responses"))
	}
	facts = append(facts, fact("tool descriptions", fmt.Sprintf("%d bytes in every request", m.agent.SpecBytes())))

	if u.Turns > 0 {
		cached := percent(u.CachedTok, u.PromptTok)
		facts = append(facts,
			gauge("last request", fmt.Sprintf("%s tokens, %s of them cached (%d%%)",
				tokCount(u.PromptTok), tokCount(u.CachedTok), cached), cached),
			fact("all requests", fmt.Sprintf("%d turns · %s prompt tokens · %s from the cache (%d%%)",
				u.Turns, tokCount(u.TotalPrompt), tokCount(u.CachedTotal), percent(u.CachedTotal, u.TotalPrompt))),
			fact("generated", fmt.Sprintf("%s tokens", tokCount(u.GenTok))),
			fact("measured here", fmt.Sprintf("%.1f bytes per token", u.BytesPerTok)))
		if u.Acceptance > 0 {
			facts = append(facts, fact("speculative decoding", fmt.Sprintf("%d%% of the draft's tokens kept (%d of %d)",
				int(u.Acceptance*100+0.5), u.DraftOK, u.Draft)))
		}
	}
	r := &report{title: "ykode · this session", facts: facts}
	if ctxSize > 0 && u.EstTok*100 >= ctxSize*ctxWarnPct {
		r.note = "this window is nearly full: /compact folds the conversation into a summary"
	}
	return r
}

// keys: a report only answers closing keys.
func (r *report) keys(msg tea.Msg) (tea.Cmd, overlayNext, bool) {
	key, isKey := msg.(tea.KeyMsg)
	if !isKey {
		return nil, stayOpen, false
	}
	switch key.String() {
	case "esc", "q", "enter", "ctrl+c":
		return nil, closeOverlay, true
	}
	return nil, stayOpen, true
}

func (r *report) view(width, height int) string {
	inner := overlayInner(width)
	nameW := 0
	for _, f := range r.facts {
		nameW = max(nameW, len(f.name))
	}
	room := max(height-6, 3) // the box, the title, the note and the hint

	var body strings.Builder
	body.WriteString(setupTitle.Render(r.title) + setupDim.Render("   what this run reads and what it costs") + "\n\n")
	for i, f := range r.facts {
		if i == room {
			body.WriteString("  " + setupDim.Render("… the rest have no room on this screen") + "\n")
			break
		}
		row := "  " + styleDim.Render(padTo(f.name, nameW)) + "  " + f.value
		if f.gauge != "" {
			row += "  " + f.gauge
		}
		body.WriteString(cutToWidth(row, inner) + "\n")
	}
	if r.note != "" {
		body.WriteString("\n" + styleWarn.Render(cutToWidth(r.note, inner)))
	}
	body.WriteString("\n" + setupDim.Render("esc closes"))
	return place(width, height, body.String())
}

// bar is a share of something, in cells: what a percentage says, seen as how much room is left.
func bar(pct, cells int) string {
	filled := min(max(pct*cells/100, 0), cells)
	return strings.Repeat(planCellDone, filled) + strings.Repeat(planCellRest, cells-filled)
}

// percent is a's share of b in whole percent; nothing of nothing is nothing.
func percent(a, b int) int {
	if b <= 0 {
		return 0
	}
	return a * 100 / b
}
