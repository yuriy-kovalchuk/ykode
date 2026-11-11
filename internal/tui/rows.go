package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	noticeChars = 200
	toolRowMark = 7  // the indent, the mark and the gaps around the name and the note
	toolRowArgs = 12 // what the note keeps even on a screen too narrow for it
)

// The session is read inside a margin: two columns at the left, one column held back from the right
// edge so a row is never the terminal's own problem, and the ask that opened a turn keeps the whole
// width - it is a band, and a band that stops short reads as a mistake rather than a margin.
const (
	messageLead  = "  "
	messageRight = 1
)

// rows is the session as the screen reads it: one element per row. Only the blocks that changed are
// drawn again, and the rows are only rebuilt from there; a different screen starts the whole session
// again.
func (t *transcript) rows(width int) []string {
	if width != t.width {
		t.width, t.stale = width, 0
		t.rowsCache, t.rowStart = nil, nil
	}
	if t.stale < len(t.rowStart) {
		// what was redrawn takes a different number of rows, so everything below it moves
		t.rowsCache = t.rowsCache[:t.rowStart[t.stale]]
		t.rowStart = t.rowStart[:t.stale]
	}
	for i := len(t.rowStart); i < len(t.entries); i++ {
		t.rowStart = append(t.rowStart, len(t.rowsCache))
		t.rowsCache = append(t.rowsCache, t.draw(&t.entries[i], width)...)
	}
	t.stale = len(t.entries)
	return t.rowsCache
}

// hasPending says output arrived that has not been drawn yet: a reader who scrolled up is told with
// a mark rather than by the screen jumping under them.
func (t *transcript) hasPending() bool { return t.stale < len(t.entries) }

// render is the whole session as one string, for reading it back rather than drawing it.
func (t *transcript) render(width int) string {
	return strings.TrimRight(strings.Join(t.rows(width), "\n"), "\n")
}

// draw is one entry as the rows the screen sees, ending with the blank row a block leaves behind.
// Everything but the ask sits inside the session's margin, and a blank row stays blank: padding it
// would leave spaces on a row that says nothing.
func (t *transcript) draw(e *entry, width int) []string {
	inner := max(width-lipgloss.Width(messageLead)-messageRight, 1)
	switch e.who {
	case whoUser:
		return append(promptRows(*e, width), "") // the band: edge to edge
	case whoAnswer:
		return append(e.laid.draw(e.text, inner, messageLead), "")
	case whoSummary:
		rows := []string{messageLead + styleDim.Render("  the older turns, folded into this")}
		return append(append(rows, e.laid.draw(e.text, inner, messageLead)...), "")
	case whoThink:
		// the gutter is laid on after the wrap, so the wrap keeps room for it on any screen
		return append(e.thought.wrap(e.text, max(inner-4, 1), thinkingRow), "")
	}
	var rows []string
	switch e.who {
	case whoOutput:
		rows = append(indent(wrapKept(e.text, max(inner-2, 1)), "  "), "")
	case whoDone:
		rows = []string{styleDim.Render(fmt.Sprintf("  %s · %s", clock(e.at), duration(e.took))), ""}
	case whoNotice:
		for _, row := range wrap(oneLine(e.text, noticeChars), max(inner-2, 1)) {
			rows = append(rows, styleDim.Render("· "+row))
		}
	case whoTool:
		rows = append(rows, toolRow(*e, inner))
		rows = append(rows, changeRows(e.call.change, 0, inner)...)
		rows = append(rows, tailRows(e.call.tail, e.call.note, inner)...)
	case whoDraft:
		rows = draftRows(drafted(e), t.root, inner)
	}
	for i, row := range rows {
		if row != "" {
			rows[i] = messageLead + row
		}
	}
	return rows
}

// thinkingRow is one row of what the model thought: behind a gutter, in the hand thinking is written in.
func thinkingRow(row string) string {
	return messageLead + gutterMark() + styleThink.Render(row)
}

// promptRows is what was asked, as a band across the whole screen: the ask then reads apart
// from the answer even when it runs over several lines.
func promptRows(e entry, width int) []string {
	when := clock(e.at) // an ask carried over from an earlier run has no hour to show
	lines := wrap(strings.TrimSpace(e.text), width-promptEdge)
	if when != "" {
		lines[len(lines)-1] = rightAligned(lines[len(lines)-1], when, width-promptEdge)
	}
	for i, line := range lines {
		lines[i] = styleUserLine.Render(padTo(userMark+" "+line, width))
	}
	return lines
}

// promptEdge and userMark are the band's left edge: the mark and the space after it.
const (
	promptEdge = 2
	userMark   = "▌"
)

// toolRow is one call on one row, with no ground behind it: the mark at its left carries the state
// (a coloured glyph is read before the words are, and a whole row of green or red is noise), the tool's
// name is bold and what it touched follows. The row is always one row: what a call read, wrote or
// printed is not on it, only what it touched and what came back.
func toolRow(e entry, width int) string {
	mark, ink := "▸", styleDim // asked for, and not answered yet
	switch {
	case e.failed:
		mark, ink = "✗", styleBad
	case !e.asked:
		mark, ink = "✓", styleOK
	}
	return callHead(mark, ink, e.tool, e.call.note, width)
}

// callHead is a call's one row: its mark, its name, and a note cut so the row never wraps.
func callHead(mark string, ink lipgloss.Style, tool, note string, width int) string {
	room := max(width-toolRowMark-lipgloss.Width(tool), toolRowArgs)
	note = cutToWidth(oneLine(note, noticeChars), room)
	return "  " + ink.Render(mark) + " " + styleBold.Render(tool) + " " + styleDim.Render(note)
}

// draftRows is a call being written: its head, then the newest lines of what it writes.
func draftRows(d draft, root string, width int) []string {
	shown, written, bytes := d.preview()
	rows := []string{callHead("✎", styleAccent, d.tool, draftHead(shorten(d.target(), root), written, bytes), width)}
	for _, line := range shown {
		rows = append(rows, gutterMark()+styleDim.Render(cutToWidth(line, width-4)))
	}
	return rows
}

// gutterMark leads a row that is not the answer, such as thinking or a call's preview.
func gutterMark() string { return styleDim.Render("  ┊ ") }
