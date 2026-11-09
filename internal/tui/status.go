package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// ctx fullness above these is shown as a warning, then as a problem
const (
	ctxWarnPct = 60
	ctxBadPct  = 85
)

// status is what the two footer rows report.
type status struct {
	root      string // the project the session works in
	branch    string // what HEAD points at there; "" outside a repository
	model     string
	thinking  string // how deeply the model thinks on the turns that follow
	usage     agent.Usage
	ctxSize   int
	prefill   float64 // tokens per second the server read the prompt at
	decode    float64 // tokens per second it writes at
	estimated bool    // both were measured on this side while the reply streamed, so they carry a ~
	running   bool
	waiting   int
	elapsed   time.Duration // how long the running prompt has been working
	took      time.Duration // how long the last prompt took, once it finished
	spinner   string
	keysHint  bool // say that ? shows the keys
	width     int  // 0 leaves the two sides unaligned
}

// rootRoom and modelRoom are how much of the path and the model name the session line keeps when
// the screen is short: a readable part of each beats the whole of one pushed off the edge.
const (
	rootRoom  = 60
	modelRoom = 40
)

// factSep separates the facts on the left from what is happening on the right.
var factSep = styleDim.Render(" · ")

// sessionLine is the footer's top row: where the session runs, and which model and depth answer.
func sessionLine(s status) string {
	where := shortenHome(s.root)
	if s.branch != "" {
		where += " (" + s.branch + ")"
	}
	depth := s.thinking
	if depth == llm.ThinkingOff {
		depth = "thinking off"
	}
	return align([]string{styleDim.Render(oneLine(where, rootRoom))},
		[]string{styleAccent.Render(oneLine(s.model, modelRoom)), styleDim.Render(depth)}, s.width)
}

// statusLine is the footer's second row: what the conversation has cost, and what is happening now.
func statusLine(s status) string {
	return align(statusFacts(s), statusActivity(s), s.width)
}

// align spreads the facts and the activity across the width, dropping facts from the front of
// the left group when the screen cannot hold both; what is happening now is never dropped.
func align(facts, activity []string, width int) string {
	left, right := strings.Join(facts, factSep), strings.Join(activity, factSep)
	if width == 0 {
		return left + "   " + right
	}
	for len(facts) > 0 {
		if gap := width - lipgloss.Width(left) - lipgloss.Width(right); gap >= 2 {
			return left + strings.Repeat(" ", gap) + right
		}
		facts = facts[1:]
		left = strings.Join(facts, factSep)
	}
	return right
}

// statusFacts change slowly: what the session has sent and received in all, and how full the
// window is. The window comes last, so it is the last thing a narrow screen drops.
func statusFacts(s status) []string {
	var facts []string
	if s.usage.TotalPrompt > 0 {
		facts = append(facts, styleDim.Render("↑")+tokCount(s.usage.TotalPrompt))
	}
	if s.usage.GenTok > 0 {
		facts = append(facts, styleDim.Render("↓")+tokCount(s.usage.GenTok))
	}
	if s.usage.CachedTotal > 0 {
		facts = append(facts, styleDim.Render("R")+tokCount(s.usage.CachedTotal))
	}
	if s.usage.PromptTok > 0 {
		facts = append(facts, styleDim.Render(fmt.Sprintf("cache %d%%", s.usage.CachedTok*100/s.usage.PromptTok)))
	}
	if s.usage.Acceptance > 0 {
		facts = append(facts, styleDim.Render(fmt.Sprintf("draft %d%%", int(s.usage.Acceptance*100+0.5))))
	}
	used := s.usage.EstTok
	if s.ctxSize > 0 {
		pct := used * 100 / s.ctxSize
		facts = append(facts, fullness(pct).Render(fmt.Sprintf("ctx %s/%s %d%%", tokCount(used), tokCount(s.ctxSize), pct)))
	} else {
		facts = append(facts, styleDim.Render(fmt.Sprintf("ctx %s", tokCount(used))))
	}
	return facts
}

// statusActivity is what is happening now: the running prompt and its clock, and the server's speed.
func statusActivity(s status) []string {
	var now []string
	switch {
	case s.running:
		work := s.spinner + " " + duration(s.elapsed)
		if s.waiting > 0 {
			work += fmt.Sprintf(", %d waiting", s.waiting)
		}
		now = append(now, styleWorking.Render(work))
	case s.waiting > 0:
		now = append(now, fmt.Sprintf("%d waiting", s.waiting))
	case s.took > 0:
		now = append(now, styleOK.Render("✓ done in "+duration(s.took)))
	}
	if s.prefill > 0 {
		now = append(now, styleDim.Render("↑ ")+rate(s.prefill, false))
	}
	if s.decode > 0 {
		now = append(now, styleDim.Render("↓ ")+rate(s.decode, s.estimated))
	}
	if s.keysHint {
		now = append(now, styleDim.Render("? keys"))
	}
	return now
}

// shortenHome writes a path under $HOME from ~, so the root and its branch fit beside the model.
func shortenHome(dir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return dir
	}
	if dir == home {
		return "~"
	}
	sep := string(filepath.Separator)
	if !strings.HasPrefix(dir, home+sep) {
		return dir
	}
	return "~" + sep + strings.TrimPrefix(dir, home+sep)
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

// tokCount writes a token count the way it is read at a glance: 900, 9.5k, 12k, 131k, 3.1M.
func tokCount(n int) string {
	switch {
	case n >= 10000000:
		return fmt.Sprintf("%dM", n/1000000)
	case n >= 1000000:
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	case n >= 10000:
		return fmt.Sprintf("%dk", n/1000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// status is what the footer reports right now.
func (m *model) status() status {
	s := status{root: m.root, branch: m.branch, model: m.agent.Model(), thinking: m.agent.ThinkingLevel(),
		usage: m.agent.Usage(), ctxSize: m.agent.CtxSize(), prefill: m.prefill,
		decode: m.decode, estimated: m.estimated, running: m.running, waiting: len(m.agent.Waiting()), took: m.took, spinner: m.spin.View(),
		keysHint: !m.showKeys, width: m.width}
	if m.running {
		s.elapsed = time.Since(m.started)
	}
	return s
}

// rate is a speed in tokens per second; a measured one says so with a ~, because the server's own
// figure, which replaces it when the reply ends, is the exact one.
func rate(perSecond float64, estimated bool) string {
	if estimated {
		return fmt.Sprintf("~%.0f tok/s", perSecond)
	}
	return fmt.Sprintf("%.0f tok/s", perSecond)
}
