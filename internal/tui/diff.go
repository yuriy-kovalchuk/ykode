package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// A change is shown whole under the call that made it: the rows are drawn once and kept, and a frame
// only ever copies the ones in view, so a long change costs the session rows to scroll past, not
// time. (Measured: three to four milliseconds for every thousand lines, once, and again when the terminal
// changes width.) The tool that made the change decides how much of a very long one it hands over.
const (
	diffLead = "    " // a change sits under the call row that made it
	diffTab  = "   "
	diffGap  = "⋯" // where one hunk ends and the next begins
)

// holdsDiff says a call answered with a change: a line about what it did, then the hunks.
func holdsDiff(result string) bool { return strings.Contains(result, "\n@@ ") }

// summaryLine is what a call says it did, before the change itself.
func summaryLine(result string) string {
	head, _, _ := strings.Cut(result, "\n")
	return head
}

// diffLine is one line of a change as it is read out of the hunks: which side it is on, what it says,
// and where it sits in the file.
type diffLine struct {
	side   byte   // ' ' unchanged, '-' went, '+' came, '@' the gap between two hunks, '#' a note the tool left
	text   string // tabs already opened out
	number int    // its line in the file: the old file's for a line that went, the new one's otherwise
	strong [2]int // the bytes of text the line it was changed from, or to, does not share
}

// readChange reads the hunks of a unified diff into lines that know their numbers. A change with no
// hunk headers - one put together from a call's arguments, before the file was touched - has none.
func readChange(change string) []diffLine {
	var lines []diffLine
	before, after, hunks := 0, 0, 0
	for _, line := range strings.Split(strings.TrimRight(change, "\n"), "\n") {
		switch {
		case line == "" || line[0] == '\\': // "\ No newline at end of file" is not a line of the file
		case strings.HasPrefix(line, "@@"):
			before, after = hunkStarts(line)
			if hunks++; hunks > 1 {
				lines = append(lines, diffLine{side: '@'})
			}
		case line[0] == '-':
			lines = append(lines, diffLine{side: '-', text: diffText(line[1:]), number: before})
			before = lineAfter(before)
		case line[0] == '+':
			lines = append(lines, diffLine{side: '+', text: diffText(line[1:]), number: after})
			after = lineAfter(after)
		case line[0] == ' ':
			lines = append(lines, diffLine{side: ' ', text: diffText(line[1:]), number: after})
			before, after = lineAfter(before), lineAfter(after)
		default:
			lines = append(lines, diffLine{side: '#', text: line})
		}
	}
	markWords(lines)
	return lines
}

// lineAfter is the line after n, and nowhere when the change never said where it was.
func lineAfter(n int) int {
	if n == 0 {
		return 0
	}
	return n + 1
}

// hunkStarts reads where a hunk begins on each side from its header, "@@ -12,6 +12,7 @@".
func hunkStarts(header string) (before, after int) {
	for _, field := range strings.Fields(header) {
		start, _, _ := strings.Cut(field[1:], ",")
		n, err := strconv.Atoi(start)
		if err != nil {
			continue
		}
		switch field[0] {
		case '-':
			before = n
		case '+':
			after = n
		}
	}
	return before, after
}

// changeSize is how many lines a change brought and how many it took, as a row says it: +12 -3.
func changeSize(change string) string {
	came, went := 0, 0
	for _, line := range strings.Split(change, "\n") {
		switch {
		case strings.HasPrefix(line, "+"):
			came++
		case strings.HasPrefix(line, "-"):
			went++
		}
	}
	var size []string
	if came > 0 {
		size = append(size, fmt.Sprintf("+%d", came))
	}
	if went > 0 {
		size = append(size, fmt.Sprintf("-%d", went))
	}
	return strings.Join(size, " ")
}

// markWords finds, where lines that went are followed by as many that came, what changed inside each
// pair: an edit is most often a word or two on a line, and that is where the eye should land.
func markWords(lines []diffLine) {
	for i := 0; i < len(lines); {
		went := i
		for i < len(lines) && lines[i].side == '-' {
			i++
		}
		came := i
		for i < len(lines) && lines[i].side == '+' {
			i++
		}
		if i == went {
			i++ // neither side: an unchanged line, a gap
			continue
		}
		if came-went != i-came {
			continue // not line for line: which replaced which would be a guess
		}
		for pair := 0; pair < came-went; pair++ {
			a, b := &lines[went+pair], &lines[came+pair]
			a.strong, b.strong = changedSpans(a.text, b.text)
		}
	}
}

// changedSpans is the part of each of two lines the other does not share: what is left between the
// words they begin with in common and the words they end with in common. Two lines with little in
// common are two different lines, and marking most of each would mark nothing.
func changedSpans(a, b string) (inA, inB [2]int) {
	head := sharedWords(a, b, false)
	tail := sharedWords(a[head:], b[head:], true)
	shared := len(strings.TrimSpace(a[:head])) + len(strings.TrimSpace(a[len(a)-tail:]))
	if longer := max(len(strings.TrimSpace(a)), len(strings.TrimSpace(b))); shared*5 < longer*2 {
		return inA, inB
	}
	return [2]int{head, len(a) - tail}, [2]int{head, len(b) - tail}
}

// sharedWords is how many bytes two lines share at their start, or at their end, counted in whole
// words: "timeout=500" and "timeout=100" share "timeout=", not that and half a number.
func sharedWords(a, b string, fromEnd bool) int {
	at := func(s string, i int) byte {
		if fromEnd {
			return s[len(s)-1-i]
		}
		return s[i]
	}
	whole := 0 // how much is shared up to the last place a word ended on both sides
	for i := 0; ; i++ {
		endA, endB := i == len(a), i == len(b)
		switch {
		case endA && endB:
			return i
		case endA:
			if !wordByte(at(b, i)) {
				return i
			}
			return whole
		case endB:
			if !wordByte(at(a, i)) {
				return i
			}
			return whole
		}
		ca, cb := at(a, i), at(b, i)
		if ca != cb {
			if !wordByte(ca) && !wordByte(cb) {
				return i
			}
			return whole
		}
		if !wordByte(ca) {
			whole = i + 1
		}
	}
}

// wordByte says a byte is part of a word. Every byte of a character outside ASCII counts, so a span
// never begins or ends inside one.
func wordByte(c byte) bool {
	return c == '_' || c >= 0x80 || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// changeRows draws the change a call made the way a review tool does: every line with its number in
// the file, a line that went on a red ground and one that came on a green one, each running to the
// edge of the screen, and the words that differ between a line and the one that replaced it on a
// stronger shade of the same ground. Unchanged lines around them are left as they are.
// limit is the most rows drawn, for a place with little room, and 0 draws them all.
func changeRows(change string, limit int, width int) []string {
	if change == "" {
		return nil
	}
	lines := readChange(change)
	digits := 0
	for _, line := range lines {
		if line.number > 0 {
			digits = max(digits, len(strconv.Itoa(line.number)))
		}
	}
	sides := diffSides()
	shown := len(lines)
	if limit > 0 {
		shown = min(shown, limit)
	}
	rows := make([]string, 0, shown+1)
	for _, line := range lines[:shown] {
		number := ""
		if digits > 0 {
			number = fmt.Sprintf("%*d ", digits, line.number)
		}
		switch line.side {
		case '@':
			rows = append(rows, styleDim.Render(diffLead+strings.Repeat(" ", len(number))+diffGap))
		case '#':
			rows = append(rows, styleDim.Render(diffLead+cutToWidth(line.text, width-len(diffLead))))
		case '-':
			rows = append(rows, diffRow(sides.del, sides.delWord, number+"- ", line, width))
		case '+':
			rows = append(rows, diffRow(sides.add, sides.addWord, number+"+ ", line, width))
		default:
			room := max(width-len(diffLead)-len(number)-3, 1) // the last column is left clear for the cursor
			rows = append(rows, diffLead+styleDim.Render(number)+"  "+cutToWidth(line.text, room))
		}
	}
	if over := len(lines) - shown; over > 0 {
		note := cutToWidth(fmt.Sprintf("… %d more lines of the change", over), width-len(diffLead))
		rows = append(rows, styleDim.Render(diffLead+note))
	}
	return rows
}

// diffRow is one changed line: its number and its sign, then its words, on a ground that runs to the
// edge of the screen. The words that differ from the other side sit on the stronger shade.
func diffRow(ground, strong lipgloss.Style, lead string, line diffLine, width int) string {
	room := max(width-len(diffLead)-len(lead)-1, 1) // the last column is left clear for the cursor
	text := cutToWidth(line.text, room)
	kept := len(text) // how much of the line survived the cut, as bytes of the line itself
	if text != line.text {
		kept = len(strings.TrimSuffix(text, "…"))
	}
	from, to := min(line.strong[0], kept), min(line.strong[1], kept)
	row := diffLead + ground.Render(lead+text[:from])
	if to > from {
		row += strong.Render(text[from:to])
	}
	rest := text[to:]
	if fill := width - len(diffLead) - lipgloss.Width(lead) - lipgloss.Width(text); fill > 0 {
		rest += strings.Repeat(" ", fill)
	}
	if rest != "" {
		row += ground.Render(rest)
	}
	return row
}

// diffText keeps a source line as long as the screen can read it: tabs are what made the columns,
// and a tab counted as one cell moves every row of the change out of line.
func diffText(text string) string {
	return strings.ReplaceAll(text, "\t", diffTab)
}
