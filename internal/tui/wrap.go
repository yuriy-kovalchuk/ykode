package tui

import (
	"strings"
	"time"

	"fmt"
	"github.com/charmbracelet/lipgloss"
)

// oneLine flattens text to a row of at most max columns: a CJK glyph or an emoji takes two, so the
// measure that matters is the screen, not the runes.
func oneLine(text string, max int) string {
	return cutToWidth(strings.Join(strings.Fields(text), " "), max)
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

// wrapKept breaks a line only where it has to: a line that fits is left exactly as it was written,
// so a table of commands keeps its columns, and only a line that had to break is re-flowed under
// its own indent.
func wrapKept(text string, width int) string {
	if width < 20 {
		width = 20
	}
	var rows []string
	for _, line := range strings.Split(text, "\n") {
		if lipgloss.Width(line) <= width {
			rows = append(rows, line)
			continue
		}
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		row := ""
		flush := func() {
			if row != "" {
				rows = append(rows, lead+row)
				row = ""
			}
		}
		for _, word := range strings.Fields(line) {
			if row != "" && lipgloss.Width(lead+row)+1+lipgloss.Width(word) > width {
				flush()
			}
			if row != "" {
				row += " "
			}
			row += word
		}
		flush()
	}
	return strings.Join(rows, "\n")
}

func indent(text, pad string) string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

// clock is a message's time of day; the date is the session's, so it is left out.
// clock is the hour of a turn. A turn carried over from an earlier run has no hour to show.
func clock(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.Format("15:04")
}

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

// cutToWidth keeps a plain line inside the screen, ending in … when something was cut. The line is
// measured once and what is dropped is measured as it goes: a long tool result is cut for every
// redraw, and measuring the shrinking line again per character costs the line squared.
func cutToWidth(s string, width int) string {
	if width <= 1 || lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	over := lipgloss.Width(s) - width // the … keeps a column of its own
	for len(r) > 0 && over >= 0 {
		over -= lipgloss.Width(string(r[len(r)-1]))
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
