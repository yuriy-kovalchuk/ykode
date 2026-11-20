package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	noticeChars = 200
	toolRowMark = 7  // the indent, the mark and the gaps around the name and the note
	toolRowArgs = 12 // what the note keeps even on a screen too narrow for it
)

// the session reads inside a margin: two columns left, one off the right edge; the ask spans it
const (
	messageLead  = "  "
	messageRight = 1
)

// rows is the session as one element per row, drawing again only the blocks that changed
func (t *transcript) rows(width int) []string {
	if width != t.width || t.gen != paletteGen {
		t.width, t.gen, t.stale = width, paletteGen, 0
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

// hasPending says output awaits drawing: a scrolled-up reader sees a mark, not a jump
func (t *transcript) hasPending() bool { return t.stale < len(t.entries) }

// render is the whole session as one string, for reading it back rather than drawing it.
func (t *transcript) render(width int) string {
	return strings.TrimRight(strings.Join(t.rows(width), "\n"), "\n")
}

// draw is one entry as rows plus its trailing blank; all but the ask sits in the margin
func (t *transcript) draw(e *entry, width int) []string {
	inner := max(width-lipgloss.Width(messageLead)-messageRight, 1)
	switch e.who {
	case whoUser:
		return append(promptRows(*e, width), "") // the card: edge to edge
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
		rows = []string{styleOK.Render("╰ ✓ ") + styleBold.Render("done in "+duration(e.took)) + styleDim.Render(" · "+clock(e.at)), ""}
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

// promptRows is the ask as a card across the screen; a carried-over ask has no hour
func promptRows(e entry, width int) []string {
	when := ""
	if at := clock(e.at); at != "" {
		when = styleDim.Render(at)
	}
	return card(styleAccent.Render("you"), when, wrap(strings.TrimSpace(e.text), max(width-4, 1)), width)
}

// toolRow is one call on one row: mark, bold tool name, what it touched, and nothing else
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
	name := padTo(tool, toolNameWidth)
	room := max(width-toolRowMark-lipgloss.Width(name), toolRowArgs)
	note = cutToWidth(oneLine(note, noticeChars), room)
	return styleRail.Render("┃") + " " + ink.Render(mark) + " " + styleBold.Render(name) + " " + styleDim.Render(note)
}

// toolNameWidth lines the tools' names up in a column: the longest one ykode has, exec_command.
const toolNameWidth = 12

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
func gutterMark() string { return styleGutter.Render("│ ") }
