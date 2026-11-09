package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	tableast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// answers parses the CommonMark a model writes. The drawing stays ours: the palette and the
// screen's width are ykode's business, and a fence the model has not closed yet parses as code
// running to the end, which is what an answer arriving a token at a time needs.
var answers = goldmark.New(goldmark.WithExtensions(extension.Table))

// codeGutter marks the lines of a fenced block so they read as code and not prose.
const codeGutter = "  │ "

// markdown lays out the model's answer: headings, lists, tables and fenced code given their
// shape, and the marks the model wrote turned into the terminal's own words. It runs on every
// draw of an answer that is still arriving, so it stays small.
func markdown(source string, width int, blocks *highlightCache) string {
	src := []byte(source)
	doc := answers.Parser().Parse(text.NewReader(src))
	var rows []string
	for node := doc.FirstChild(); node != nil; node = node.NextSibling() {
		rows = append(rows, block(node, src, width, blocks)...)
	}
	return strings.TrimRight(strings.Join(rows, "\n"), "\n")
}

// block is a block of the answer as rows, kept inside the width it is given.
func block(node ast.Node, src []byte, width int, blocks *highlightCache) []string {
	switch n := node.(type) {
	case *ast.Heading:
		return wrap(styleBold.Render(inline(n, src)), width-2)
	case *ast.FencedCodeBlock:
		return codeRows(n, src, width, string(n.Language(src)), fenceClosed(n, src), blocks)
	case *ast.CodeBlock:
		return codeRows(n, src, width, "", true, blocks)
	case *ast.ThematicBreak:
		return []string{styleDim.Render("  " + strings.Repeat("─", min(width-4, 40)))}
	case *ast.Blockquote:
		var rows []string
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			rows = append(rows, block(child, src, width-4, blocks)...)
		}
		for i, row := range rows {
			rows[i] = "  " + styleDim.Render("┊ ") + row
		}
		return rows
	case *ast.List:
		return listRows(n, src, width, blocks)
	case *tableast.Table:
		return tableRows(n, src, width)
	case *ast.HTMLBlock:
		return dimLines(n.Lines(), src, width)
	default:
		return wrap(inline(node, src), width-2)
	}
}

// listRows are a list's entries: the model's own marker in a column, each entry wrapped under
// itself, and any block an entry holds indented beneath it.
func listRows(list *ast.List, src []byte, width int, blocks *highlightCache) []string {
	var rows []string
	number := list.Start
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		lead := "  " + string(list.Marker) + " "
		if list.IsOrdered() {
			lead = "  " + strconv.Itoa(number) + ". "
			number++
		}
		var body []string
		for child := item.FirstChild(); child != nil; child = child.NextSibling() {
			body = append(body, block(child, src, width-len(lead), blocks)...)
		}
		pad := strings.Repeat(" ", len(lead))
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
// a language we have a grammar for is coloured; one that names nothing, or a language nobody has a
// grammar for, is drawn exactly as the model wrote it.
func codeRows(node ast.Node, src []byte, width int, language string, closed bool, blocks *highlightCache) []string {
	var rows []string
	if language != "" {
		rows = append(rows, styleDim.Render("    "+language))
	}
	if coloured := highlightedCode(blockText(node.Lines(), src), language, closed, blocks); coloured != nil {
		return append(rows, gutterRows(coloured, width, codeGutter)...)
	}
	return append(rows, markedLines(node.Lines(), src, width, codeGutter)...)
}

// fenceClosed says whether a fence has been shut by its closing ``` yet. goldmark reads the rest of the
// answer as the block when it has not, so an unterminated one ends where the answer does: its last line
// runs to the end of the source. A block still arriving is worth colouring once it is over, not while it
// is being written, because the lines it has so far are a new question on every token.
func fenceClosed(node ast.Node, src []byte) bool {
	lines := node.(*ast.FencedCodeBlock).Lines()
	if lines.Len() == 0 {
		return false
	}
	return lines.At(lines.Len()-1).Stop < len(src)
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

// markedLines writes source lines out as they were written, each one inside the width.
func markedLines(segments *text.Segments, src []byte, width int, mark string) []string {
	var lines []string
	for i := 0; i < segments.Len(); i++ {
		segment := segments.At(i)
		lines = append(lines, strings.TrimRight(string(segment.Value(src)), " \r\n"))
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return gutterRows(lines, width, mark)
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

// dimLines shows what the answer wrapped in raw html: the tags kept rather than dropped, so
// nothing the model wrote goes missing on the way to the screen.
func dimLines(segments *text.Segments, src []byte, width int) []string {
	return markedLines(segments, src, width, "")
}

// inline is a block's text run: emphasis, code spans and links turned into the terminal's words.
func inline(node ast.Node, src []byte) string {
	var out strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.Emphasis:
			words := inline(n, src)
			switch n.Level {
			case 1:
				words = styleItalic.Render(words)
			case 2:
				words = styleBold.Render(words)
			default:
				words = styleBold.Render(styleItalic.Render(words))
			}
			out.WriteString(words)
		case *ast.CodeSpan:
			out.WriteString(styleCode.Render(leafText(n, src)))
		case *ast.Link:
			out.WriteString(inline(n, src) + " " + styleDim.Render("("+string(n.Destination)+")"))
		case *ast.AutoLink:
			out.WriteString(styleCode.Render(string(n.URL(src))))
		case *ast.Text:
			out.WriteString(textWords(n, src))
			if n.SoftLineBreak() || n.HardLineBreak() {
				out.WriteString(" ") // a break the model typed: the row wraps where it needs to
			}
		case *ast.String:
			out.WriteString(string(n.Value))
		default:
			out.WriteString(leafText(n, src))
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

// tableRows keep a table readable without a column engine: one row per line, its cells apart,
// the heading named once.
func tableRows(table *tableast.Table, src []byte, width int) []string {
	var rows []string
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		var cells []string
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			cells = append(cells, inline(cell, src))
		}
		if len(cells) == 0 {
			continue
		}
		line := strings.Join(cells, styleDim.Render(" · "))
		if _, header := row.(*tableast.TableHeader); header {
			line = styleBold.Render(line)
		}
		rows = append(rows, indent(wrap(line, width-2), "  ")...)
	}
	return rows
}
