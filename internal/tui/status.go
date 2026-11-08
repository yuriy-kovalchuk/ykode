package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// ctx fullness above these is shown as a warning, then as a problem
const (
	ctxWarnPct = 60
	ctxBadPct  = 85
)

// status is what the status line reports.
type status struct {
	model    string
	usage    agent.Usage
	ctxSize  int
	prefill  float64 // tokens per second the server read the prompt at
	decode   float64 // tokens per second it writes at
	running  bool
	queued   int
	elapsed  time.Duration // how long the running prompt has been working
	took     time.Duration // how long the last prompt took, once it finished
	spinner  string
	keysHint bool // say that ? shows the keys
	width    int  // 0 leaves the two sides unaligned
}

// statusLine puts the session's facts on the left and what is happening now on the right.
func statusLine(s status) string {
	sep := styleDim.Render(" · ")
	facts, activity := statusFacts(s), strings.Join(statusActivity(s), sep)
	if s.width == 0 {
		return strings.Join(facts, sep) + "   " + activity
	}
	// a narrow screen drops facts from the end; what is happening now always stays
	for len(facts) > 0 {
		left := strings.Join(facts, sep)
		if gap := s.width - lipgloss.Width(left) - lipgloss.Width(activity); gap >= 2 {
			return left + strings.Repeat(" ", gap) + activity
		}
		facts = facts[:len(facts)-1]
	}
	return activity
}

// statusFacts change slowly: which model, how full the window is, how much the server had cached.
func statusFacts(s status) []string {
	facts := []string{styleAccent.Render(oneLine(s.model, 40))}
	used := s.usage.EstTok
	if s.usage.Turns == 0 {
		used = 0
	}
	if s.ctxSize > 0 {
		pct := used * 100 / s.ctxSize
		facts = append(facts, fullness(pct).Render(fmt.Sprintf("ctx %s/%s %d%%", kilo(used), kilo(s.ctxSize), pct)))
	} else {
		facts = append(facts, styleDim.Render(fmt.Sprintf("ctx %s", kilo(used))))
	}
	if s.usage.PromptTok > 0 {
		facts = append(facts, styleDim.Render(fmt.Sprintf("cache %d%%", s.usage.CachedTok*100/s.usage.PromptTok)))
	}
	if s.usage.Acceptance > 0 {
		facts = append(facts, styleDim.Render(fmt.Sprintf("draft %d%%", int(s.usage.Acceptance*100+0.5))))
	}
	return facts
}

// statusActivity is what is happening now: the running prompt and its clock, and the server's speed.
func statusActivity(s status) []string {
	var now []string
	switch {
	case s.running:
		work := s.spinner + " " + duration(s.elapsed)
		if s.queued > 0 {
			work += fmt.Sprintf(", %d queued", s.queued)
		}
		now = append(now, styleWorking.Render(work))
	case s.queued > 0:
		now = append(now, fmt.Sprintf("%d queued", s.queued))
	case s.took > 0:
		now = append(now, styleOK.Render("✓ done in "+duration(s.took)))
	}
	if s.prefill > 0 {
		now = append(now, styleDim.Render("↑ ")+fmt.Sprintf("%.0f", s.prefill))
	}
	if s.decode > 0 {
		now = append(now, styleDim.Render("↓ ")+fmt.Sprintf("%.0f tok/s", s.decode))
	}
	if s.keysHint {
		now = append(now, styleDim.Render("? keys"))
	}
	return now
}

// fullness colours how much of the window is used: green, then yellow, then red.
func fullness(pct int) lipgloss.Style {
	switch {
	case pct >= ctxBadPct:
		return styleBad
	case pct >= ctxWarnPct:
		return styleWarn
	}
	return styleOK
}

// duration reads at a glance: 7s, 1m12s, 1h03m.
func duration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// kilo writes 131072 as 131k: the numbers are read at a glance, not compared.
func kilo(n int) string {
	switch {
	case n >= 100000:
		return fmt.Sprintf("%dk", n/1000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
