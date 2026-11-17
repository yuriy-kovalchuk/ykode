package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// oneLine flattens text to a row of at most max columns, where CJK and emoji take two
func oneLine(text string, max int) string {
	return cutToWidth(strings.Join(strings.Fields(text), " "), max)
}

// wrap breaks text to width, keeping words whole and cutting runs that have no spaces
func wrap(text string, width int) []string {
	var rows []string
	for _, paragraph := range strings.Split(text, "\n") {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			for _, piece := range cutWide(word, width) {
				if line != "" && lipgloss.Width(line)+1+lipgloss.Width(piece) > width {
					rows = append(rows, line)
					line = ""
				}
				if line == "" {
					line = piece
				} else {
					line += " " + piece
				}
			}
		}
		rows = append(rows, line) // an empty paragraph keeps its blank row
	}
	if strings.Contains(text, "\x1b[") {
		carryStyles(rows)
	}
	return rows
}

const styleReset = "\x1b[0m"

// carryStyles keeps a style over a break on both sides, since rows draw on their own
func carryStyles(rows []string) {
	open := ""
	for i, row := range rows {
		still := stylesLeftOpen(open, row)
		if row != "" {
			if open != "" {
				row = open + row
			}
			if still != "" {
				row += styleReset
			}
			rows[i] = row
		}
		open = still
	}
}

// stylesLeftOpen answers which styles a row still has switched on at its end
func stylesLeftOpen(open, row string) string {
	for {
		at := strings.Index(row, "\x1b[")
		if at < 0 {
			return open
		}
		row = row[at+2:]
		end := strings.IndexFunc(row, func(r rune) bool { return r >= 0x40 && r <= 0x7e })
		if end < 0 {
			return open
		}
		if row[end] == 'm' {
			if params := row[:end]; params == "" || params == "0" {
				open = ""
			} else {
				open += "\x1b[" + row[:end+1]
			}
		}
		row = row[end+1:]
	}
}

// flow wraps prose still arriving: finished paragraphs are wrapped once and their rows kept
type flow struct {
	width int
	upto  int      // how much of the text the rows below cover
	rows  []string // the paragraphs that ended, as the screen draws them
}

// wrap breaks text to width and passes every row through dress, which gives a row its mark and style.
func (f *flow) wrap(text string, width int, dress func(string) string) []string {
	if width != f.width || len(text) < f.upto {
		*f = flow{width: width}
	}
	if end := strings.LastIndexByte(text, '\n'); end >= f.upto {
		for _, row := range wrap(text[f.upto:end], width) {
			f.rows = append(f.rows, dress(row))
		}
		f.upto = end + 1
	}
	rows := f.rows[:len(f.rows):len(f.rows)]
	for _, row := range wrap(text[f.upto:], width) {
		rows = append(rows, dress(row))
	}
	return rows
}

// cutWide breaks a run wider than the screen into pieces that fit; a fitting word is itself
func cutWide(word string, width int) []string {
	if width < 1 || lipgloss.Width(word) <= width {
		return []string{word}
	}
	return strings.Split(ansi.Hardwrap(word, width, true), "\n")
}

// wrapKept breaks only where it must, so a table keeps its columns, re-flowing under its indent
var labelHang = 14 // a label this short keeps its value lined up when the row carries on

//x // a label this short keeps its value lined up when the row has to carry on

func wrapKept(text string, width int) []string {
	var rows []string
	for _, line := range strings.Split(text, "\n") {
		if lipgloss.Width(line) <= width {
			rows = append(rows, line)
			continue
		}
		lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		head := strings.TrimLeft(line, " \t")
		// a labelled value keeps its value lined up when the row carries on
		if name, value, ok := strings.Cut(head, ": "); ok && lipgloss.Width(name) <= labelHang {
			gap := lead + strings.Repeat(" ", lipgloss.Width(name)+2)
			parts := wrap(value, max(width-lipgloss.Width(gap), 8))
			rows = append(rows, lead+name+": "+parts[0])
			rows = append(rows, indent(wrap(strings.Join(parts[1:], " "), max(width-lipgloss.Width(gap), 8)), gap)...)
			continue
		}
		rows = append(rows, indent(wrap(head, max(width-lipgloss.Width(lead), 8)), lead)...)
	}
	return rows
}

func indent(rows []string, pad string) []string {
	for i, row := range rows {
		rows[i] = pad + row
	}
	return rows
}

// clock is the hour of a turn, the date the session's; a carried-over turn has no hour
func clock(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.Format("15:04")
}

// padTo fills a row out to width so a style laid over it covers the whole row.
func padTo(text string, width int) string {
	if gap := width - lipgloss.Width(text); gap > 0 {
		return text + strings.Repeat(" ", gap)
	}
	return text
}

// cutToWidth keeps a plain line inside the screen with …, cutting between characters
func cutToWidth(s string, width int) string {
	if width <= 1 {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// duration reads at a glance: 7s, 1m12s, 1h03m.
func duration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}
