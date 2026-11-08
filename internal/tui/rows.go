package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

const (
	noticeChars = 200
	toolRowMark = 5  // the mark, the gaps around the arguments and the gap before the result
	toolRowArgs = 12 // what the arguments keep, even when the result wants the row
)

// rows is the session as the screen reads it: one element per row. A tool row is one row by default;
// expanded, it also shows what the call was asked to do. Only the blocks that changed are drawn
// again, and the rows are only rebuilt from there; a different screen, or rows that moved, starts
// the whole session again.
func (t *transcript) rows(width int, expanded bool) []string {
	if width != t.width || expanded != t.expanded {
		t.width, t.expanded, t.lines, t.stale = width, expanded, nil, 0
		t.rowsCache, t.rowed, t.rowStart = nil, 0, nil
	}
	for len(t.lines) < len(t.entries) {
		t.lines = append(t.lines, "")
	}
	from := t.stale
	for i := t.stale; i < len(t.entries); i++ {
		t.lines[i] = draw(t.entries[i], width, expanded)
	}
	t.stale = len(t.entries)
	t.reflow(from)
	return t.rowsCache
}

// reflow puts drawn blocks into rows from the first block that changed. A block's own ending newline
// is not a row of its own, so the rows read exactly as the blocks joined with "" did.
func (t *transcript) reflow(from int) {
	if from > t.rowed {
		from = t.rowed
	}
	if from < t.rowed {
		t.rowsCache, t.rowed = t.rowsCache[:t.rowStart[from]], from
	}
	for i := t.rowed; i < len(t.entries); i++ {
		t.rowStart = append(t.rowStart, len(t.rowsCache))
		block := strings.Split(t.lines[i], "\n")
		if last := len(block) - 1; last >= 0 && block[last] == "" {
			block = block[:last]
		}
		t.rowsCache = append(t.rowsCache, block...)
	}
	t.rowed = len(t.entries)
}

// hasPending says output arrived that has not been drawn yet: a reader who scrolled up is told with
// a mark rather than by the screen jumping under them.
func (t *transcript) hasPending() bool { return t.stale < len(t.entries) }

// render is the whole session as one string, for reading it back rather than drawing it.
func (t *transcript) render(width int, expanded bool) string {
	return strings.TrimRight(strings.Join(t.rows(width, expanded), "\n"), "\n")
}

// draw is one entry as the screen sees it, ending with the blank line a block leaves behind.
func draw(e entry, width int, expanded bool) string {
	switch e.who {
	case whoUser:
		return promptBlock(e, width) + "\n\n"
	case whoSummary:
		return styleDim.Render("  the older turns, folded into this") + "\n" + markdown(e.text, width) + "\n\n"
	case whoOutput:
		return indent(wrapKept(e.text, width-2), "  ") + "\n\n"
	case whoDone:
		return fmt.Sprintf("%s\n\n", styleDim.Render(fmt.Sprintf("  %s · %s", clock(e.at), duration(e.took))))
	case whoThink:
		return fmt.Sprintf("%s\n\n", gutter(wrap(e.text, width-4), styleThink))
	case whoAnswer:
		return markdown(e.text, width) + "\n\n"
	case whoNotice:
		return styleDim.Render(indent(wrap(oneLine(e.text, noticeChars), width-2), "· ")) + "\n"
	case whoTool:
		row := toolRow(e, width) + "\n"
		if rows := diffRows(e.result, width, expanded); rows != nil {
			row += strings.Join(rows, "\n") + "\n"
		}
		if expanded && e.args != "" {
			row += openArgs(e.args, width) + "\n"
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
	when := clock(e.at) // an ask carried over from an earlier run has no hour to show
	lines := strings.Split(wrap(strings.TrimSpace(e.text), width-promptEdge), "\n")
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

// toolRow is one call on one row: the arguments give way to the result, and a result too wide for
// the row is cut as well, so a call never runs past the edge of the screen.
func toolRow(e entry, width int) string {
	row := struct {
		mark, note string
		markStyle  lipgloss.Style
		noteStyle  lipgloss.Style
	}{"→", "working…", styleAccent, styleDim}
	switch {
	case e.failed:
		row.mark, row.note, row.markStyle, row.noteStyle = "✗", e.result, styleBad, styleBad
	case e.result != "":
		// a change is a block of its own under the row, so the row keeps only what the call said
		note := e.result
		if holdsDiff(note) {
			note = summaryLine(note)
		}
		row.mark, row.note, row.markStyle, row.noteStyle = "✓", note, styleOK, styleResult
	}
	room := width - toolRowMark - lipgloss.Width(e.tool) - lipgloss.Width(row.note)
	if room < toolRowArgs {
		room = toolRowArgs
		row.note = cutToWidth(row.note, max(width-toolRowMark-lipgloss.Width(e.tool)-room, 4))
	}
	return fmt.Sprintf("%s %s %s  %s", row.markStyle.Render(row.mark), styleToolName.Render(e.tool),
		styleDim.Render(oneLine(tools.Describe(e.tool, e.args), room)), row.noteStyle.Render(row.note))
}

// expandedRows is how much of one argument an opened call shows before saying how much more there is.
const expandedRows = 14

// openArgs is a call's arguments opened up under ctrl+o: a field per row, and long text shown as
// the lines it holds rather than as escaped JSON.
func openArgs(args string, width int) string {
	var fields map[string]any
	if err := json.Unmarshal([]byte(args), &fields); err != nil {
		return indent(wrap(oneLine(args, argChars), width-4), "    ")
	}
	var rows []string
	for _, key := range argOrder(fields) {
		if text, isText := fields[key].(string); isText {
			lines := strings.Split(text, "\n")
			if len(lines) > expandedRows {
				lines = append(lines[:expandedRows], fmt.Sprintf("… %d rows more", len(lines)-expandedRows))
			}
			rows = append(rows, styleDim.Render("  "+key+":"))
			for _, line := range lines {
				rows = append(rows, "    "+cutToWidth(line, width-4))
			}
			continue
		}
		rows = append(rows, styleDim.Render("  "+key+":")+" "+cutToWidth(plain(fields[key]), max(width-8, 10)))
	}
	return strings.Join(rows, "\n")
}

// argOrder puts what a call touches first, so an opened call starts where the risk is.
func argOrder(fields map[string]any) []string {
	risky := []string{"path", "pattern", "command", "glob", "offset", "limit"}
	order := make([]string, 0, len(fields))
	for _, key := range risky {
		if _, ok := fields[key]; ok {
			order = append(order, key)
		}
	}
	rest := make([]string, 0, len(fields))
	for key := range fields {
		if !slices.Contains(risky, key) {
			rest = append(rest, key)
		}
	}
	slices.Sort(rest)
	return append(order, rest...)
}

// plain writes an argument that is not text as one line.
func plain(value any) string {
	switch v := value.(type) {
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return oneLine(fmt.Sprint(v), argChars)
		}
		return oneLine(string(raw), argChars)
	}
}

// argChars is how much of one argument is worth keeping on a row.
const argChars = 2000

// draftRows is a call being written: its head, then the newest lines of what it writes.
func draftRows(d draft, width int) string {
	shown, written, bytes := d.preview()
	row := fmt.Sprintf("%s %s %s", styleWorking.Render("✎"), styleToolName.Render(d.tool),
		styleDim.Render(draftHead(d.target(), written, bytes)))
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
