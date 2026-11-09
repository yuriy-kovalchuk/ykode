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
		t.width, t.expanded, t.stale = width, expanded, 0
		t.rowsCache, t.rowStart = nil, nil
	}
	if t.stale < len(t.rowStart) {
		// what was redrawn takes a different number of rows, so everything below it moves
		t.rowsCache = t.rowsCache[:t.rowStart[t.stale]]
		t.rowStart = t.rowStart[:t.stale]
	}
	for i := len(t.rowStart); i < len(t.entries); i++ {
		t.rowStart = append(t.rowStart, len(t.rowsCache))
		t.rowsCache = append(t.rowsCache, draw(&t.entries[i], width, expanded)...)
	}
	t.stale = len(t.entries)
	return t.rowsCache
}

// hasPending says output arrived that has not been drawn yet: a reader who scrolled up is told with
// a mark rather than by the screen jumping under them.
func (t *transcript) hasPending() bool { return t.stale < len(t.entries) }

// render is the whole session as one string, for reading it back rather than drawing it.
func (t *transcript) render(width int, expanded bool) string {
	return strings.TrimRight(strings.Join(t.rows(width, expanded), "\n"), "\n")
}

// draw is one entry as the screen sees it, ending with the blank row a block leaves behind.
func draw(e *entry, width int, expanded bool) []string {
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
		if rows := diffRows(e.result, width, expanded); rows != nil {
			block += strings.Join(rows, "\n") + "\n"
		}
		if expanded && e.args != "" {
			block += openArgs(e.args, width) + "\n"
		}
	case whoDraft:
		block = draftRows(drafted(e), width) + "\n"
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

// toolRow is one call on one row, sitting in a band whose colour is its state: the arguments give
// way to the result, and a result too wide for the row is cut as well, so a call never runs past the
// edge of the screen.
func toolRow(e entry, width int) string {
	mark, note, band := "→", "working…", surfaces().wait
	switch {
	case e.failed:
		mark, note, band = "✗", e.result, surfaces().fail
	case e.result != "":
		// a change is a block of its own under the row, so the row keeps only what the call said
		note = e.result
		if holdsDiff(note) {
			note = summaryLine(note)
		}
		mark, band = "✓", surfaces().done
	}
	room := width - toolRowMark - lipgloss.Width(e.tool) - lipgloss.Width(note)
	if room < toolRowArgs {
		room = toolRowArgs
		note = cutToWidth(note, max(width-toolRowMark-lipgloss.Width(e.tool)-room, 4))
	}
	// the band does the quieting, so nothing inside it is also faded: a faded mark on a coloured
	// ground is near invisible on some terminals
	head := fmt.Sprintf("%s %s %s  %s", mark, styleBold.Render(e.tool),
		oneLine(tools.Describe(e.tool, e.args), room), note)
	return band.Width(width).Render(head)
}

// expandedRows is how much of one argument an opened call shows before saying how much more there is.
const expandedRows = 14

// openArgs is a call's arguments opened up under ctrl+o: a field per row, and long text shown as
// the lines it holds rather than as escaped JSON.
func openArgs(args string, width int) string {
	var fields map[string]any
	if err := json.Unmarshal([]byte(args), &fields); err != nil {
		return strings.Join(indent(wrap(oneLine(args, argChars), width-4), "    "), "\n")
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
	head := fmt.Sprintf("%s %s %s", "✎", styleBold.Render(d.tool), draftHead(d.target(), written, bytes))
	row := surfaces().wait.Width(width).Render(head)
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
