package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	noticeChars = 200
	toolRowTail = 20 // what is left for the result after the tool name on a row
)

// render turns the transcript into lines for the viewport. A tool row is one line by
// default; expanded, it also shows what the call was asked to do. Rows keep their drawing between
// draws; a different screen, or rows that moved, starts them again.
func (t *transcript) render(width int, expanded bool) string {
	if width != t.width || expanded != t.expanded {
		t.width, t.expanded, t.lines, t.stale = width, expanded, nil, 0
	}
	for len(t.lines) < len(t.entries) {
		t.lines = append(t.lines, "")
	}
	for i := t.stale; i < len(t.entries); i++ {
		t.lines[i] = draw(t.entries[i], width, expanded)
	}
	t.stale = len(t.entries)
	return strings.TrimRight(strings.Join(t.lines, ""), "\n")
}

// draw is one entry as the screen sees it, ending with the blank line a block leaves behind.
func draw(e entry, width int, expanded bool) string {
	switch e.who {
	case whoUser:
		return promptBlock(e, width) + "\n\n"
	case whoOutput:
		return fmt.Sprintf("%s\n\n", indent(e.text, "  "))
	case whoDone:
		return fmt.Sprintf("%s\n\n", styleDim.Render(fmt.Sprintf("  %s · %s", clock(e.at), duration(e.took))))
	case whoThink:
		return fmt.Sprintf("%s\n\n", gutter(wrap(e.text, width-4), styleThink))
	case whoAnswer:
		return fmt.Sprintf("%s\n\n", e.text)
	case whoNotice:
		return fmt.Sprintf("%s\n", styleDim.Render("· "+oneLine(e.text, noticeChars)))
	case whoTool:
		row := toolRow(e, width) + "\n"
		if expanded && e.args != "" {
			row += fmt.Sprintf("%s\n", indent(wrap(e.args, width-2), "  "))
		}
		return row
	case whoDraft:
		return draftRows(draft{call: e.call, tool: e.tool, args: e.args}, width) + "\n"
	}
	return ""
}

// promptBlock is what was asked, as a band across the whole screen: the ask then reads apart
// from the answer even when it runs over several lines.
func promptBlock(e entry, width int) string {
	when := clock(e.at)
	lines := strings.Split(wrap(strings.TrimSpace(e.text), width-promptEdge), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	lines[len(lines)-1] = rightAligned(lines[len(lines)-1], when, width-promptEdge)
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

// toolRow is one call on one row: the arguments give way to the result.
func toolRow(e entry, width int) string {
	mark, tail := styleAccent.Render("→"), styleDim.Render("working…")
	switch {
	case e.failed:
		mark, tail = styleBad.Render("✗"), styleBad.Render(e.result)
	case e.result != "":
		mark, tail = styleOK.Render("✓"), e.result
	}
	return fmt.Sprintf("%s %s %s  %s", mark, styleToolName.Render(e.tool), styleDim.Render(oneLine(e.args, rowArgs(width, e.result))), tail)
}

// draftRows is a call being written: its head, then the newest lines of what it writes.
func draftRows(d draft, width int) string {
	lines, written, size := d.preview()
	row := fmt.Sprintf("%s %s %s", styleWorking.Render("✎"), styleToolName.Render(d.tool),
		styleDim.Render(draftHead(d.target(), written, size)))
	for i, line := range lines {
		lines[i] = cutToWidth(line, width-4)
	}
	if len(lines) == 0 {
		return row
	}
	return row + "\n" + gutter(strings.Join(lines, "\n"), styleDim)
}

// gutter marks a block that is not the answer, such as thinking or a call's preview.
func gutter(text string, style lipgloss.Style) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = styleDim.Render("  ┊ ") + style.Render(line)
	}
	return strings.Join(lines, "\n")
}

// rowArgs is what of the call fits on one row once the tool name and its result are placed.
func rowArgs(width int, result string) int {
	room := width - len(result) - len(oneLine("", 0)) - toolRowTail
	if room < 20 {
		room = 20
	}
	return room
}
