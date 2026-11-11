package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// answers parses the CommonMark a model writes, with the tables, struck-out text and checklists
// models write as well. The drawing stays ours: the palette and the screen's width are ykode's
// business, and a fence the model has not closed yet parses as code running to the end, which is what
// an answer arriving a token at a time needs.
var answers = goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.TaskList))

// codeGutter indents the lines of a fenced block. It marks nothing: a rule down the side of code
// cannot be selected away, so every copy of a block would carry it.
const codeGutter = "    "

// layout is an answer as the screen last laid it out. An answer arrives a token at a time and is
// drawn many times a second while it does, but text added at the end only ever changes the last block
// of it: every block before that is laid out once and its rows kept. So a frame costs the block being
// written, not the answer so far.
type layout struct {
	width   int
	lead    string
	settled int       // how many of the answer's blocks the rows below hold
	rows    []string  // the rows of the blocks that will not change
	code    codeCache // the coloured code of this answer
}

// draw lays out the model's answer: headings, lists, tables and fenced code given their shape, and
// the marks the model wrote turned into the terminal's own words. Every row starts with lead, and a
// blank row stays blank.
func (l *layout) draw(source string, width int, lead string) []string {
	if l.width != width || l.lead != lead {
		*l = layout{width: width, lead: lead, code: l.code} // the colours do not depend on the width
	}
	src := []byte(source)
	doc := answers.Parser().Parse(text.NewReader(src))
	if doc.ChildCount() < l.settled {
		l.settled, l.rows = 0, nil // not the answer these rows were drawn from
	}
	var last []string
	at := 0
	for node := doc.FirstChild(); node != nil; node, at = node.NextSibling(), at+1 {
		if at < l.settled {
			continue
		}
		rows := block(node, src, width, &l.code)
		if at > 0 {
			rows = append([]string{""}, rows...) // blocks stand apart, as paragraphs do on a page
		}
		for i, row := range rows {
			if row != "" {
				rows[i] = lead + row
			}
		}
		if node.NextSibling() == nil {
			last = rows // the block still being written: drawn again when more of it arrives
			break
		}
		l.rows = append(l.rows, rows...)
		l.settled++
	}
	return append(l.rows[:len(l.rows):len(l.rows)], last...)
}

// block is a block of the answer as rows, kept inside the width it is given.
func block(node ast.Node, src []byte, width int, code *codeCache) []string {
	switch n := node.(type) {
	case *ast.Heading:
		// the title of an answer is underlined, its sections bold, and what is under those bold and
		// slanted: three levels are as many as a terminal can tell apart
		return wrap(inline(n, src, ink{bold: true, underline: n.Level == 1, italic: n.Level > 2}), width-2)
	case *ast.FencedCodeBlock:
		return codeRows(n, src, width, string(n.Language(src)), fenceClosed(n, src), code)
	case *ast.CodeBlock:
		return codeRows(n, src, width, "", true, code)
	case *ast.ThematicBreak:
		return []string{styleDim.Render("  " + strings.Repeat("─", max(min(width-4, 40), 1)))}
	case *ast.Blockquote:
		var rows []string
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			rows = append(rows, block(child, src, width-4, code)...)
		}
		for i, row := range rows {
			rows[i] = "  " + styleDim.Render("┊ ") + row
		}
		return rows
	case *ast.List:
		return listRows(n, src, width, code, false)
	case *extast.Table:
		return tableRows(n, src, width)
	case *ast.HTMLBlock:
		// what the answer wrapped in raw html: the tags kept rather than dropped, so nothing the model
		// wrote goes missing on the way to the screen
		return gutterRows(sourceLines(n.Lines(), src), width, "")
	default:
		return wrap(inline(node, src, ink{}), width-2)
	}
}

// listRows are a list's entries: a bullet or the entry's number in a column, each entry wrapped under
// itself, and any block an entry holds indented beneath it. A list inside an entry sits under that
// entry's words, with a bullet of its own kind.
func listRows(list *ast.List, src []byte, width int, code *codeCache, nested bool) []string {
	margin, bullet := "  ", "•"
	if nested {
		margin, bullet = "", "◦" // the entry above already set it in
	}
	var rows []string
	number := list.Start
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		lead := margin + bullet + " "
		if list.IsOrdered() {
			lead = margin + strconv.Itoa(number) + ". "
			number++
		}
		room := width - lipgloss.Width(lead)
		var body []string
		for child := item.FirstChild(); child != nil; child = child.NextSibling() {
			if inner, ok := child.(*ast.List); ok {
				body = append(body, listRows(inner, src, room, code, true)...)
				continue
			}
			body = append(body, block(child, src, room, code)...)
		}
		pad := strings.Repeat(" ", lipgloss.Width(lead))
		for i, row := range body {
			if i == 0 {
				rows = append(rows, lead+row)
				continue
			}
			rows = append(rows, pad+row)
		}
	}
	return rows
}

// codeRows are the lines of a code block, with the language named once above them. A block that names
// a language we have a grammar for is coloured, while it is still arriving as much as once it closed;
// one that names nothing, or a language nobody has a grammar for, is drawn exactly as the model wrote it.
func codeRows(node ast.Node, src []byte, width int, language string, closed bool, cache *codeCache) []string {
	var rows []string
	if language != "" {
		rows = append(rows, styleDim.Render(codeGutter+language))
	}
	code := blockText(node.Lines(), src)
	if !closed {
		code = withoutClosingFence(code)
	}
	lines := cache.rows(code, language, closed)
	if lines == nil {
		lines = strings.Split(code, "\n")
	}
	return append(rows, gutterRows(lines, width, codeGutter)...)
}

// fenceClosed says whether a fence has been shut by its closing ``` yet. goldmark reads the rest of the
// answer as the block when it has not, so an unterminated one ends where the answer does: its last line
// runs to the end of the source.
func fenceClosed(node ast.Node, src []byte) bool {
	lines := node.(*ast.FencedCodeBlock).Lines()
	if lines.Len() == 0 {
		return false
	}
	return lines.At(lines.Len()-1).Stop < len(src)
}

// withoutClosingFence drops the closing fence while it is still arriving: its first backtick is, for
// the length of a token, the last line of the code, and a block that ends in a stray ` for a frame
// reads as a flicker.
func withoutClosingFence(code string) string {
	at := strings.LastIndexByte(code, '\n') + 1
	last := strings.TrimSpace(code[at:])
	if last == "" || (strings.Trim(last, "`") != "" && strings.Trim(last, "~") != "") {
		return code
	}
	return strings.TrimRight(code[:at], "\n")
}

// blockText is the source a block's lines hold, joined as it was written. A block written with the
// carriage returns of a windows file is read as the same lines the screen shows elsewhere.
func blockText(segments *text.Segments, src []byte) string {
	var joined strings.Builder
	for i := 0; i < segments.Len(); i++ {
		segment := segments.At(i)
		joined.Write(segment.Value(src))
	}
	return strings.TrimRight(strings.ReplaceAll(joined.String(), "\r\n", "\n"), "\n")
}

// sourceLines are a block's lines as they were written.
func sourceLines(segments *text.Segments, src []byte) []string {
	lines := make([]string, 0, max(segments.Len(), 1))
	for i := 0; i < segments.Len(); i++ {
		segment := segments.At(i)
		lines = append(lines, strings.TrimRight(string(segment.Value(src)), " \r\n"))
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}

// gutterRows puts the mark on every row of a block and keeps each one inside the width.
func gutterRows(lines []string, width int, mark string) []string {
	room := width - lipgloss.Width(mark)
	rows := make([]string, len(lines))
	for i, line := range lines {
		rows[i] = mark + cutToWidth(line, room)
	}
	return rows
}

// ink is how a run of words inside a block is written. Marks nest - bold around code, a link inside
// emphasis - and a style closed in the middle of another would switch the outer one off with it, so
// each run of words is written once, with everything that holds for it.
type ink struct{ bold, italic, code, struck, underline bool }

func (k ink) write(words string) string {
	if k == (ink{}) || words == "" {
		return words
	}
	style := lipgloss.NewStyle().Bold(k.bold).Italic(k.italic)
	if k.code {
		style = style.Inherit(styleCode)
	}
	written := style.Render(words)
	// a line under the words or through them is opened by hand: lipgloss draws either a character at a
	// time, an escape before and after each, and a heading would be ten times its own length
	lines := ""
	if k.underline {
		lines += "\x1b[4m"
	}
	if k.struck {
		lines += "\x1b[9m"
	}
	if lines == "" || lipgloss.ColorProfile() == termenv.Ascii {
		return written
	}
	return lines + strings.TrimSuffix(written, styleReset) + styleReset
}

// inline is a block's text run: emphasis, code spans and links turned into the terminal's words.
func inline(node ast.Node, src []byte, in ink) string {
	var out strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.Emphasis:
			inner := in
			if n.Level == 1 {
				inner.italic = true
			} else {
				inner.bold = true
			}
			out.WriteString(inline(n, src, inner))
		case *extast.Strikethrough:
			inner := in
			inner.struck = true
			out.WriteString(inline(n, src, inner))
		case *ast.CodeSpan:
			inner := in
			inner.code = true
			out.WriteString(inner.write(leafText(n, src)))
		case *ast.Link:
			words, address := inline(n, src, in), string(n.Destination)
			out.WriteString(words)
			if address != "" && address != leafText(n, src) { // an address written as its own words is said once
				out.WriteString(" " + styleDim.Render("("+address+")"))
			}
		case *ast.AutoLink:
			inner := in
			inner.code = true
			out.WriteString(inner.write(string(n.URL(src))))
		case *extast.TaskCheckBox:
			if n.IsChecked {
				out.WriteString(styleOK.Render("[x]") + " ")
			} else {
				out.WriteString(styleDim.Render("[ ]") + " ")
			}
		case *ast.Text:
			out.WriteString(in.write(textWords(n, src)))
			if n.SoftLineBreak() || n.HardLineBreak() {
				// a break the model typed is a break on the screen: it writes a value a line, a step a
				// line, and run together into one paragraph they stop being readable as data
				out.WriteString("\n")
			}
		case *ast.RawHTML:
			raw := string(n.Segments.Value(src))
			if tag := strings.ToLower(strings.Join(strings.Fields(raw), "")); tag == "<br>" || tag == "<br/>" {
				out.WriteString("\n") // the only way to break a line inside a table's cell
			} else {
				out.WriteString(styleDim.Render(raw)) // any other tag is kept, so nothing written goes missing
			}
		case *ast.String:
			out.WriteString(in.write(string(n.Value)))
		default:
			out.WriteString(in.write(leafText(n, src)))
		}
	}
	return out.String()
}

// textWords is a run of words with the punctuation escapes the model wrote dropped.
func textWords(text *ast.Text, src []byte) string {
	segment := text.Segment
	return string(util.UnescapePunctuations(segment.Value(src)))
}

// leafText is what a run of inlines says in its own words, markdown escapes already dropped and
// a line break the model typed turned into a space the row wraps at.
func leafText(node ast.Node, src []byte) string {
	var out strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.Text:
			out.WriteString(textWords(n, src))
			if n.SoftLineBreak() || n.HardLineBreak() {
				out.WriteString(" ")
			}
		case *ast.String:
			out.WriteString(string(n.Value))
		default:
			out.WriteString(leafText(n, src))
		}
	}
	return out.String()
}

// tableMinColumn is the narrowest a column of a table is drawn: under this a cell is a letter a row.
const tableMinColumn = 4

// tableRows draw a table as a grid: a rule around it and between its columns, the heading bold over a
// rule of its own. A table wider than the screen has its widest columns narrowed and their cells
// wrapped inside them; one that still cannot fit - more columns than the screen has room to rule -
// is written as records instead, each row's cells under each other with their headings beside them.
func tableRows(table *extast.Table, src []byte, width int) []string {
	var cells [][]string
	var natural []int
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		_, heading := row.(*extast.TableHeader)
		var line []string
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			words := strings.TrimSpace(inline(cell, src, ink{bold: heading}))
			if len(line) == len(natural) {
				natural = append(natural, 1)
			}
			for _, part := range strings.Split(words, "\n") {
				natural[len(line)] = max(natural[len(line)], lipgloss.Width(part))
			}
			line = append(line, words)
		}
		if len(line) > 0 {
			cells = append(cells, line)
		}
	}
	if len(cells) == 0 {
		return nil
	}
	// every column costs its words, a space either side of them and a rule; one more rule closes the row
	widths := fitColumns(natural, width-2-3*len(natural)-1)
	if widths == nil {
		return tableRecords(cells, width)
	}
	// each cell as the rows it takes in its column
	wrapped := make([][][]string, len(cells))
	tall := false
	for i, line := range cells {
		wrapped[i] = make([][]string, len(widths))
		for column := range widths {
			words := ""
			if column < len(line) {
				words = line[column]
			}
			wrapped[i][column] = wrap(words, widths[column])
			tall = tall || len(wrapped[i][column]) > 1
		}
	}
	rule := func(left, mid, right string) string {
		parts := make([]string, len(widths))
		for column, w := range widths {
			parts[column] = strings.Repeat("─", w+2)
		}
		return "  " + styleDim.Render(left+strings.Join(parts, mid)+right)
	}
	bar := styleDim.Render("│")
	rows := []string{rule("┌", "┬", "┐")}
	for i, line := range wrapped {
		height := 0
		for _, cell := range line {
			height = max(height, len(cell))
		}
		for at := 0; at < height; at++ {
			var out strings.Builder
			out.WriteString("  " + bar)
			for column, cell := range line {
				words := ""
				if at < len(cell) {
					words = cell[at]
				}
				gap := strings.Repeat(" ", max(widths[column]-lipgloss.Width(words), 0))
				if column < len(table.Alignments) && table.Alignments[column] == extast.AlignRight {
					words, gap = gap+words, ""
				}
				out.WriteString(" " + words + gap + " " + bar)
			}
			rows = append(rows, out.String())
		}
		switch {
		case i == len(wrapped)-1:
			rows = append(rows, rule("└", "┴", "┘"))
		case i == 0 || tall: // rows that run over several lines need a rule to stay apart
			rows = append(rows, rule("├", "┼", "┤"))
		}
	}
	return rows
}

// fitColumns narrows the columns of a table until they fit the room, taking from the widest first:
// a column of numbers keeps its width and the column of prose beside it gives. It answers nil when
// even columns at their narrowest would not fit.
func fitColumns(natural []int, room int) []int {
	widths := append([]int(nil), natural...)
	total := 0
	for _, w := range widths {
		total += w
	}
	for total > room {
		widest := 0
		for column, w := range widths {
			if w > widths[widest] {
				widest = column
			}
		}
		if widths[widest] <= tableMinColumn {
			return nil
		}
		widths[widest]--
		total--
	}
	return widths
}

// tableRecords is a table too wide to rule, written a row at a time: each cell on a line of its own
// under the heading of its column, a blank row between two rows of the table.
func tableRecords(cells [][]string, width int) []string {
	var rows []string
	for i, line := range cells[1:] {
		if i > 0 {
			rows = append(rows, "")
		}
		for column, words := range line {
			heading := ""
			if column < len(cells[0]) {
				heading = cells[0][column] + styleDim.Render(": ")
			}
			rows = append(rows, indent(wrap(heading+strings.ReplaceAll(words, "\n", " "), width-2), "  ")...)
		}
	}
	if len(cells) == 1 {
		rows = indent(wrap(strings.Join(cells[0], styleDim.Render(" · ")), width-2), "  ")
	}
	return rows
}
