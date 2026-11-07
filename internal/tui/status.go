package tui

import (
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// statusLine is the one line that says what the session costs: which model, how full the
// context is, how much of the prompt the server already had, and how fast it answers.
func statusLine(model string, u agent.Usage, ctxSize int, decodeTokSec float64, running bool, queued int) string {
	parts := []string{oneLine(model, 40)}

	used := u.EstTok
	if u.Turns == 0 {
		used = 0
	}
	if ctxSize > 0 {
		parts = append(parts, fmt.Sprintf("ctx %s/%s %d%%", kilo(used), kilo(ctxSize), used*100/ctxSize))
	} else {
		parts = append(parts, fmt.Sprintf("ctx %s", kilo(used)))
	}
	if u.PromptTok > 0 {
		parts = append(parts, fmt.Sprintf("cache %d%%", u.CachedTok*100/u.PromptTok))
	}
	if decodeTokSec > 0 {
		parts = append(parts, fmt.Sprintf("%.0f tok/s", decodeTokSec))
	}
	switch {
	case running && queued > 0:
		parts = append(parts, fmt.Sprintf("working, %d queued", queued))
	case running:
		parts = append(parts, "working")
	case queued > 0:
		parts = append(parts, fmt.Sprintf("%d queued", queued))
	}
	return strings.Join(parts, " │ ")
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
