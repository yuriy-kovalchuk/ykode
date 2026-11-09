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

// wrap breaks text to width: words stay whole, and a run of characters with no spaces to break at —
// text in Chinese, a long path — is cut at the edge rather than kept as one word running off it.
func wrap(text string, width int) []string {
	var rows []string
	for _, paragraph := range strings.Split(text, "\n") {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			for _, piece := range cutWide(word, width) {
				if line != "" && lipgloss.Width(line)+1+lipgloss.Width(piece) > width {
					rows = append(rows, line)
					line = ""
				}
				if line == "" {
					line = piece
				} else {
					line += " " + piece
				}
			}
		}
		rows = append(rows, line) // an empty paragraph keeps its blank row
	}
	return rows
}

// cutWide breaks a run of characters wider than the screen into pieces that fit. A word that fits
// comes back as itself, which is what wrapping does almost always.
func cutWide(word string, width int) []string {
	if width < 1 || lipgloss.Width(word) <= width {
		return []string{word}
	}
	runes := []rune(word)
	var pieces []string
	for start := 0; start < len(runes); {
		end, used := start, 0
		for end < len(runes) && used+lipgloss.Width(string(runes[end])) <= width {
			used += lipgloss.Width(string(runes[end]))
			end++
		}
		if end == start {
			end++ // one character wider than the screen still gets a row of its own
		}
		pieces = append(pieces, string(runes[start:end]))
		start = end
	}
	return pieces
}

// wrapKept breaks a line only where it has to: a line that fits is left exactly as it was written,
// so a table of commands keeps its columns, and a line that had to break is re-flowed under its own
// indent.
func wrapKept(text string, width int) []string {
	var rows []string
	for _, line := range strings.Split(text, "\n") {
		if lipgloss.Width(line) <= width {
			rows = append(rows, line)
			continue
		}
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		rows = append(rows, indent(wrap(strings.TrimLeft(line, " \t"), width-lipgloss.Width(lead)), lead)...)
	}
	return rows
}

func indent(rows []string, pad string) []string {
	for i, row := range rows {
		rows[i] = pad + row
	}
	return rows
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
