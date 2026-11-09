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

// rows is the session as the screen reads it: one element per row. Only the blocks that changed are
// drawn again
// again, and the rows are only rebuilt from there; a different screen, or rows that moved, starts
// the whole session again.
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

// draw is one entry as the screen sees it, ending with the blank row a block leaves behind.
func (t *transcript) draw(e *entry, width int) []string {
	var block string
	switch e.who {
	case whoUser:
		block = promptBlock(*e, width) + "\n\n"
	case whoSummary:
		block = styleDim.Render("  the older turns, folded into this") + "\n" + markdown(e.text, width) + "\n\n"
	case whoOutput:
		block = strings.Join(indent(wrapKept(e.text, width-2), "  "), "\n") + "\n\n"
	case whoDone:
		block = fmt.Sprintf("%s\n\n", styleDim.Render(fmt.Sprintf("  %s · %s", clock(e.at), duration(e.took))))
	case whoThink:
		block = fmt.Sprintf("%s\n\n", gutter(strings.Join(wrap(e.text, width-4), "\n"), styleThink))
	case whoAnswer:
		block = markdown(e.text, width) + "\n\n"
	case whoNotice:
		block = styleDim.Render(strings.Join(indent(wrap(oneLine(e.text, noticeChars), width-2), "· "), "\n")) + "\n"
	case whoTool:
		block = toolRow(*e, width) + "\n"
		if rows := changeRows(e.call.change, e.call.left, width); rows != nil {
			block += strings.Join(rows, "\n") + "\n"
		}
		if rows := tailRows(e.call.tail, e.call.note, width); rows != nil {
			block += strings.Join(rows, "\n") + "\n"
		}
	case whoDraft:
		block = draftRows(drafted(e), t.root, width) + "\n"
	}
	return blockRows(block)
}

// blockRows turns what a block drew into the rows it takes. A block's own ending newline is not a row
// of its own, so the rows read exactly as the blocks joined with "" did.
func blockRows(block string) []string {
	rows := strings.Split(block, "\n")
	if last := len(rows) - 1; last >= 0 && rows[last] == "" {
		rows = rows[:last]
	}
	return rows
}

// promptBlock is what was asked, as a band across the whole screen: the ask then reads apart
// from the answer even when it runs over several lines.
func promptBlock(e entry, width int) string {
	when := clock(e.at) // an ask carried over from an earlier run has no hour to show
	lines := wrap(strings.TrimSpace(e.text), width-promptEdge)
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	if when != "" {
		lines[len(lines)-1] = rightAligned(lines[len(lines)-1], when, width-promptEdge)
	}
	for i, line := range lines {
		lines[i] = styleUserLine.Render(padTo(userMark+" "+line, width))
	}
	return strings.Join(lines, "\n")
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
	case e.args == "":
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
func draftRows(d draft, root string, width int) string {
	shown, written, bytes := d.preview()
	row := callHead("✎", styleAccent, d.tool, draftHead(shorten(d.target(), root), written, bytes), width)
	for i, line := range shown {
		shown[i] = cutToWidth(line, width-4)
	}
	if len(shown) == 0 {
		return row
	}
	return row + "\n" + gutter(strings.Join(shown, "\n"), styleDim)
}

// gutter marks a block that is not the answer, such as thinking or a call's preview.
func gutter(text string, style lipgloss.Style) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = styleDim.Render("  ┊ ") + style.Render(line)
	}
	return strings.Join(lines, "\n")
}
