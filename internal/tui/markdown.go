package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// answers parses the CommonMark models write, with tables and checklists, drawn by ykode's palette
var answers = goldmark.New(goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.TaskList))

// codeGutter indents a fenced block; a rule down its side would come along on every copy
const codeGutter = "    "

// layout is an answer as last laid out: text added at the end changes only its last block
type layout struct {
	width   int
	lead    string
	settled int       // how many of the answer's blocks the rows below hold
	rows    []string  // the rows of the blocks that will not change
	code    codeCache // the coloured code of this answer
	gen     int       // the palette the rows and the code were coloured in
}

// draw lays out an answer: headings, lists, tables and code given shape, marks as words
func (l *layout) draw(source string, width int, lead string) []string {
	if l.gen != paletteGen {
		*l = layout{gen: paletteGen} // every row and every coloured line carries the old palette's codes
	}
	if l.width != width || l.lead != lead {
		*l = layout{width: width, lead: lead, gen: l.gen, code: l.code} // the colours do not depend on the width
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
		// the answer's title is underlined, its sections bold, and what is under them bold and slanted
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
		// raw html in an answer keeps its tags, so nothing the model wrote goes missing on the way
		return gutterRows(sourceLines(n.Lines(), src), width, "")
	default:
		return wrap(inline(node, src, ink{}), width-2)
	}
}

// listRows are entries with their bullet or number, wrapped under themselves, blocks indented
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

// codeRows are a block's lines, language named once, coloured when a grammar knows it
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

// fenceClosed says whether a fence has shut: goldmark reads the rest of the answer as the block
func fenceClosed(node ast.Node, src []byte) bool {
	lines := node.(*ast.FencedCodeBlock).Lines()
	if lines.Len() == 0 {
		return false
	}
	return lines.At(lines.Len()-1).Stop < len(src)
}

// withoutClosingFence drops a closing fence still arriving, which would flicker for a token
func withoutClosingFence(code string) string {
	at := strings.LastIndexByte(code, '\n') + 1
	last := strings.TrimSpace(code[at:])
	if last == "" || (strings.Trim(last, "`") != "" && strings.Trim(last, "~") != "") {
		return code
	}
	return strings.TrimRight(code[:at], "\n")
}

// blockText is a block's source joined as written, carriage returns read as the same lines
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

// ink writes a run of words once with all that holds for it, since marks nest and styles leak
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
	// underline and strike are opened by hand: lipgloss writes an escape per character
	lines := ""
	if k.underline {
		lines += "\x1b[4m"
	}
	if k.struck {
		lines += "\x1b[9m"
	}
	if lines == "" {
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
				// a break the model typed is a break on screen: merged into one paragraph, data stops reading
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

// leafText is a run of inlines in its own words, escapes dropped, a break turned into a space
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

// tableRows draw a grid, narrowing wide cells, or writing records when it cannot fit at all
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

// fitColumns narrows the widest columns first, answering nil when even those would not fit
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

// tableRecords is a table too wide to rule: a row at a time, each cell under its heading
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
