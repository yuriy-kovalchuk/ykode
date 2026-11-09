package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	return strings.Split(ansi.Hardwrap(word, width, true), "\n")
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

// clock is the hour of a turn; the date is the session's. A turn carried over from an earlier run
// has no hour to show.
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

// cutToWidth keeps a plain line inside the screen, ending in … when something was cut. The cut falls
// between characters, never inside a wide one.
func cutToWidth(s string, width int) string {
	if width <= 1 {
		return s
	}
	return ansi.Truncate(s, width, "…")
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
