package tui

import (
	"strings"
	"time"

	"fmt"
	"github.com/charmbracelet/lipgloss"
)

func oneLine(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return text
}

// wrap breaks a line to width without cutting words, keeping long words intact.
func wrap(text string, width int) string {
	if width < 20 {
		width = 20
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			if line != "" && len(line)+1+len(word) > width {
				lines = append(lines, line)
				line = word
				continue
			}
			if line == "" {
				line = word
			} else {
				line += " " + word
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func indent(text, pad string) string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

// clock is a message's time of day; the date is the session's, so it is left out.
func clock(at time.Time) string { return at.Format("15:04") }

// rightAligned puts tag at the end of the row when it fits, after the text when it does not.
func rightAligned(text, tag string, width int) string {
	gap := width - lipgloss.Width(text) - lipgloss.Width(tag)
	if gap < 2 {
		return text + "  " + tag
	}
	return text + strings.Repeat(" ", gap) + tag
}

// padTo fills a row out to width so a style laid over it covers the whole row.
func padTo(text string, width int) string {
	if gap := width - lipgloss.Width(text); gap > 0 {
		return text + strings.Repeat(" ", gap)
	}
	return text
}

// cutToWidth keeps a plain line inside the screen, ending in … when something was cut.
func cutToWidth(s string, width int) string {
	if width <= 1 || lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > width {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
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
