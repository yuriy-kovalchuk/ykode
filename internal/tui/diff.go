package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// How much of one change the screen shows while the session runs, and how much once the call is
// opened. A file written at once is a hundred rows of "+" and the session has to stay readable
// under it, so the rest is left to ctrl+o and the row says how much is left.
const (
	diffShown    = 14
	diffExpanded = 60
	diffLead     = "  " // a change sits under the call row that made it
	diffTab      = "   "
)

// holdsDiff says a call answered with a change: a line about what it did, then the hunks.
func holdsDiff(result string) bool { return strings.Contains(result, "\n@@ ") }

// summaryLine is what a call says it did, before the change itself.
func summaryLine(result string) string {
	head, _, _ := strings.Cut(result, "\n")
	return head
}

// diffRows draws the change a call made: what was added kept, what was removed marked, the
// unchanged context quiet, and where only part of a line changed, only that part is reversed out.
func diffRows(result string, width int, expanded bool) []string {
	if !holdsDiff(result) {
		return nil
	}
	_, body, _ := strings.Cut(result, "\n")
	limit := diffShown
	if expanded {
		limit = diffExpanded
	}
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	rows := make([]string, 0, len(lines))
	for at := 0; at < len(lines); {
		line := lines[at]
		at++
		switch {
		case line == "":
		case strings.HasPrefix(line, "@@"):
			rows = append(rows, diffRow(styleDim, "", line, width))
		case strings.HasPrefix(line, "-"):
			removed, added := hunkRun(lines, &at, line)
			if len(removed) == 1 && len(added) == 1 {
				rows = append(rows, changedPair(removed[0], added[0], width)...)
				continue
			}
			for _, gone := range removed {
				rows = append(rows, diffRow(styleDiffDel, gone[:1], gone[1:], width))
			}
			for _, fresh := range added {
				rows = append(rows, diffRow(styleDiffAdd, fresh[:1], fresh[1:], width))
			}
		case strings.HasPrefix(line, "+"):
			rows = append(rows, diffRow(styleDiffAdd, "+", line[1:], width))
		default:
			rows = append(rows, diffRow(styleDim, "", " "+line, width))
		}
	}
	if len(rows) > limit {
		left := len(rows) - limit
		note := cutToWidth(fmt.Sprintf("… %d more lines (ctrl+o)", left), width-len(diffLead))
		rows = append(rows[:limit], styleDim.Render(diffLead+note))
	}
	return rows
}

// hunkRun collects the lines that went and the lines that came after them: one change reads as one
// block, not as two lists that have to be matched by eye.
func hunkRun(lines []string, at *int, first string) (removed, added []string) {
	removed = []string{first}
	for *at < len(lines) && strings.HasPrefix(lines[*at], "-") {
		removed, *at = append(removed, lines[*at]), *at+1
	}
	for *at < len(lines) && strings.HasPrefix(lines[*at], "+") {
		added, *at = append(added, lines[*at]), *at+1
	}
	return removed, added
}

// changedPair draws a line that became another line: what stayed is quiet, what changed is reversed
// out on both sides, which is how a one word edit reads without counting characters.
func changedPair(removed, added string, width int) []string {
	gone := diffText(removed[1:])
	fresh := diffText(added[1:])
	room := width - len(diffLead) - 1
	gone, fresh = cutToWidth(gone, room), cutToWidth(fresh, room)
	head, tail := commonRun(gone, fresh)
	side := func(mark, text string, style lipgloss.Style) string {
		runes := []rune(text)
		kept, changed, rest := string(runes[:head]), string(runes[head:len(runes)-tail]), string(runes[len(runes)-tail:])
		var row strings.Builder
		row.WriteString(style.Render(diffLead + mark + kept))
		if changed != "" {
			row.WriteString(styleDiffMark.Render(changed))
		}
		if rest != "" {
			row.WriteString(style.Render(rest))
		}
		return row.String()
	}
	return []string{side("-", gone, styleDiffDel), side("+", fresh, styleDiffAdd)}
}

// commonRun measures what the two sides of a change share at each end.
func commonRun(a, b string) (head, tail int) {
	ra, rb := []rune(a), []rune(b)
	for head < len(ra) && head < len(rb) && ra[head] == rb[head] {
		head++
	}
	for tail < len(ra)-head && tail < len(rb)-head && ra[len(ra)-1-tail] == rb[len(rb)-1-tail] {
		tail++
	}
	return head, tail
}

func diffRow(style lipgloss.Style, mark, text string, width int) string {
	room := width - len(diffLead) - lipgloss.Width(mark)
	return style.Render(diffLead + mark + cutToWidth(diffText(text), room))
}

// diffText keeps a source line as long as the screen can read it: tabs are what made the columns,
// and a tab counted as one cell moves every row of the change out of line.
func diffText(text string) string {
	return strings.ReplaceAll(text, "\t", diffTab)
}
