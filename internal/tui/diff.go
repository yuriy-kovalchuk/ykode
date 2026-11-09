package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// How much of one change the screen shows. A file written at once is a hundred rows of "+" and the
// session has to stay readable under it, so the row says how much of the change it left out.
const (
	diffShown = 14
	diffLead  = "  " // a change sits under the call row that made it
	diffTab   = "   "
)

// holdsDiff says a call answered with a change: a line about what it did, then the hunks.
func holdsDiff(result string) bool { return strings.Contains(result, "\n@@ ") }

// summaryLine is what a call says it did, before the change itself.
func summaryLine(result string) string {
	head, _, _ := strings.Cut(result, "\n")
	return head
}

// changeRows draws the change a call made: what was added kept, what was removed marked, the
// unchanged context quiet, and where only part of a line changed, only that part is reversed out.
// left counts the lines of the change that were never handed over, and joins the note about the cut.
func changeRows(change string, left int, width int) []string {
	if change == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(change, "\n"), "\n")
	rows := make([]string, 0, len(lines))
	for at := 0; at < len(lines); {
		line := lines[at]
		at++
		switch {
		case line == "":
		case strings.HasPrefix(line, "@@"):
			rows = append(rows, styleDim.Render(diffLead+cutToWidth(line, width-len(diffLead))))
		case strings.HasPrefix(line, "-"):
			removed, added := hunkRun(lines, &at, line)
			if len(removed) == 1 && len(added) == 1 {
				rows = append(rows, changedPair(removed[0], added[0], width)...)
				continue
			}
			for _, gone := range removed {
				rows = append(rows, diffRow(diffSides().del, styleDelMark, gone[:1], gone[1:], width))
			}
			for _, fresh := range added {
				rows = append(rows, diffRow(diffSides().add, styleAddMark, fresh[:1], fresh[1:], width))
			}
		case strings.HasPrefix(line, "+"):
			rows = append(rows, diffRow(diffSides().add, styleAddMark, "+", line[1:], width))
		default: // an unchanged line is quiet: no ground, nothing marked
			rows = append(rows, styleDim.Render(diffLead+" "+cutToWidth(diffText(line), width-len(diffLead)-1)))
		}
	}
	if over := len(rows) - diffShown; over > 0 || left > 0 {
		note := cutToWidth(fmt.Sprintf("… %d more lines of the change", over+left), width-len(diffLead))
		rows = append(rows[:min(len(rows), diffShown)], styleDim.Render(diffLead+note))
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
	gone, fresh = cutToWidth(gone, width-len(diffLead)-1), cutToWidth(fresh, width-len(diffLead)-1)
	head, tail := commonRun(gone, fresh)
	side := func(mark, text string, ground, ink, stronger lipgloss.Style) string {
		runes := []rune(text)
		var row strings.Builder
		room := width - len(diffLead) - 1
		row.WriteString(ground.Render(diffLead) + ink.Render(mark))
		if head+tail > len(runes) { // nothing is left of the line once what stayed at both ends is taken off
			head, tail = len(runes), 0
		}
		room -= head + (len(runes) - tail - head)
		row.WriteString(ground.Render(string(runes[:head])))
		if changed := string(runes[head : len(runes)-tail]); changed != "" {
			row.WriteString(stronger.Render(cutToWidth(changed, max(room, 0))))
		}
		if rest := string(runes[len(runes)-tail:]); rest != "" {
			row.WriteString(ground.Render(rest))
		}
		return padGround(row.String(), ground, width)
	}
	sides := diffSides()
	return []string{
		side("-", gone, sides.del, styleDelMark, sides.delMark),
		side("+", fresh, sides.add, styleAddMark, sides.addMark),
	}
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

// diffRow is one line of a change: a low ground for the row, one coloured glyph for its side, and the
// words in the terminal's own colour.
func diffRow(ground, ink lipgloss.Style, mark, text string, width int) string {
	room := width - len(diffLead) - lipgloss.Width(mark) - 1 // the last column is left clear for the cursor
	return padGround(ground.Render(diffLead)+ink.Render(mark)+ground.Render(cutToWidth(diffText(text), room)), ground, width)
}

// padGround runs a row's ground to the edge of the screen, so a change reads as a band and not as a
// ground that stops where the words do.
func padGround(row string, ground lipgloss.Style, width int) string {
	if left := width - lipgloss.Width(row); left > 0 {
		return row + ground.Render(strings.Repeat(" ", left))
	}
	return row
}

// diffText keeps a source line as long as the screen can read it: tabs are what made the columns,
// and a tab counted as one cell moves every row of the change out of line.
func diffText(text string) string {
	return strings.ReplaceAll(text, "\t", diffTab)
}
