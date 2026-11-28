package tui

import "strings"

// thought is thinking as it arrives: prose in thinking's hand, a fenced block kept line for line
type thought struct {
	width int
	gen   int      // the palette the kept rows were dressed in
	upto  int      // how much of the text the kept rows cover: every part that can no longer change
	rows  []string // those parts as the screen draws them
	tail  flow     // the prose still arriving, its finished paragraphs wrapped once
}

// thoughtPart is a stretch of thinking: prose, or one fenced block from its opening fence on
type thoughtPart struct {
	text  string
	code  bool
	next  int  // where the part after it starts, counted from where the split began
	whole bool // nothing still arriving can change it: a later part follows, or its fence closed for good
}

// draw is thinking as rows behind the gutter; code keeps its lines but not colour, which is the answer's
func (t *thought) draw(text string, width int, dress func(string) string) []string {
	if width != t.width || t.gen != paletteGen || len(text) < t.upto {
		*t = thought{width: width, gen: paletteGen}
	}
	parts := thoughtParts(text[t.upto:])
	var last []string
	keeping := true // parts are kept in order, so the first that may still change ends the keeping
	for i, part := range parts {
		var rows []string
		if part.code {
			rows = thoughtCode(part.text, width, dress)
		} else {
			rows = t.tail.wrap(part.text, width, dress)
		}
		keeping = keeping && part.whole && i < len(parts)-1
		if !keeping {
			last = append(last, rows...)
			continue
		}
		if !part.code {
			t.tail = flow{} // the prose after the next fence starts its own paragraphs
		}
		t.rows = append(t.rows, rows...)
		t.upto += part.next
	}
	return append(t.rows[:len(t.rows):len(t.rows)], last...)
}

// thoughtCode is a fenced block in thinking: its language named, its lines indented and cut, never wrapped
func thoughtCode(block string, width int, dress func(string) string) []string {
	lines := strings.Split(block, "\n")
	opening := strings.TrimLeft(lines[0], " ")
	fence := fenceOpens(opening)
	lines = lines[1:]
	// the closing fence, whole or still arriving: the indent already says where the code ends
	if n := len(lines); n > 0 {
		if last := strings.TrimSpace(lines[n-1]); last != "" && strings.Trim(last, fence[:1]) == "" {
			lines = lines[:n-1]
		}
	}
	for i, line := range lines {
		lines[i] = diffText(line) // tabs expanded before the cut, so a cut row fits the screen
	}
	var rows []string
	if language := strings.TrimSpace(opening[len(fence):]); language != "" {
		rows = append(rows, dress(codeGutter+language))
	}
	for _, line := range gutterRows(lines, width, codeGutter) {
		rows = append(rows, dress(line))
	}
	return rows
}

// thoughtParts splits thinking at its fences; the newline before and after a fence belongs to the fence
func thoughtParts(text string) []thoughtPart {
	var parts []thoughtPart
	start, fence := 0, ""
	for at := 0; at < len(text); {
		end := strings.IndexByte(text[at:], '\n')
		ended := end >= 0 // a line still arriving may yet turn out to be something else
		if ended {
			end += at
		} else {
			end = len(text)
		}
		line := strings.TrimLeft(text[at:end], " ")
		switch {
		case fence == "" && fenceOpens(line) != "":
			if at > start {
				parts = append(parts, thoughtPart{text: strings.TrimSuffix(text[start:at], "\n"), next: at - start, whole: true})
			}
			start, fence = at, fenceOpens(line)
		case fence != "" && strings.HasPrefix(line, fence) && strings.Trim(line, fence[:1]+" ") == "":
			next := min(end+1, len(text))
			parts = append(parts, thoughtPart{text: text[start:end], code: true, next: next - start, whole: ended})
			start, fence = next, ""
		}
		at = end + 1
	}
	if start < len(text) {
		parts = append(parts, thoughtPart{text: text[start:], code: fence != "", next: len(text) - start})
	}
	return parts
}

// fenceOpens is the run of backticks or tildes a line opens a code block with, or "" when it opens none
func fenceOpens(line string) string {
	for _, mark := range []string{"`", "~"} {
		run := len(line) - len(strings.TrimLeft(line, mark))
		// a backtick fence's info string has no backtick, or the line is a code span, not a fence
		if run >= 3 && (mark == "~" || !strings.Contains(line[run:], "`")) {
			return line[:run]
		}
	}
	return ""
}
