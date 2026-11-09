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
	diffLead  = "    " // a change sits under the call row that made it
	diffTab   = "   "
)

// holdsDiff says a call answered with a change: a line about what it did, then the hunks.
func holdsDiff(result string) bool { return strings.Contains(result, "\n@@ ") }

// summaryLine is what a call says it did, before the change itself.
func summaryLine(result string) string {
	head, _, _ := strings.Cut(result, "\n")
	return head
}

// changeRows draws the change a call made: a removed line on a low red ground, an added one on a low
// green one, each led by a glyph in its colour, the unchanged context quiet. There is one shade per
// side and no stronger one inside a line: the words stay the terminal's own colour.
// left counts the lines of the change that were never handed over, and joins the note about the cut.
func changeRows(change string, left int, width int) []string {
	if change == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(change, "\n"), "\n")
	rows := make([]string, 0, len(lines))
	sides := diffSides()
	room := max(width-len(diffLead)-1, 1) // the last column is left clear for the cursor
	for _, line := range lines {
		switch {
		case line == "":
		case strings.HasPrefix(line, "@@"):
			rows = append(rows, styleDim.Render(diffLead+cutToWidth(line, width-len(diffLead))))
		case strings.HasPrefix(line, "-"):
			rows = append(rows, diffRow(sides.del, styleDelMark, "-", line[1:], width))
		case strings.HasPrefix(line, "+"):
			rows = append(rows, diffRow(sides.add, styleAddMark, "+", line[1:], width))
		default: // an unchanged line is quiet
			rows = append(rows, styleDim.Render(diffLead+cutToWidth(diffText(line), room)))
		}
	}
	if over := len(rows) - diffShown; over > 0 || left > 0 {
		note := cutToWidth(fmt.Sprintf("… %d more lines of the change", over+left), width-len(diffLead))
		rows = append(rows[:min(len(rows), diffShown)], styleDim.Render(diffLead+note))
	}
	return rows
}

// diffRow is one changed line: a low ground running to the edge of the screen, a glyph for its side,
// and the words in the terminal's own colour.
func diffRow(ground, ink lipgloss.Style, mark, text string, width int) string {
	room := width - len(diffLead) - lipgloss.Width(mark) - 1 // the last column is left clear for the cursor
	row := ground.Render(diffLead) + ink.Render(mark) + ground.Render(cutToWidth(diffText(text), room))
	if left := width - lipgloss.Width(row); left > 0 {
		row += ground.Render(strings.Repeat(" ", left))
	}
	return row
}

// diffText keeps a source line as long as the screen can read it: tabs are what made the columns,
// and a tab counted as one cell moves every row of the change out of line.
func diffText(text string) string {
	return strings.ReplaceAll(text, "\t", diffTab)
}
