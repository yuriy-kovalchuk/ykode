package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// what edit_file answers with: one summary line, then the change itself
const editResult = "edited src/parser.go (120 -> 148 bytes, 1 replacement)\n" +
	"@@ -12,6 +12,7 @@\n" +
	" func parse(line string) {\n" +
	"-\tif line == \"\" {\n" +
	"+\tif line == \"\" || strings.TrimSpace(line) == \"\" {\n" +
	" \t\treturn\n"

// diffLines is a change as the screen sees it: the row's sentence first, then the hunks.
func diffLines(t *testing.T, result string, width int) []string {
	t.Helper()
	_, body, _ := strings.Cut(result, "\n")
	rows := changeRows(body, 0, width)
	if len(rows) == 0 {
		t.Fatal("the change was not drawn at all")
	}
	return rows
}

// a change is drawn as a change: added lines kept, removed lines marked, context quiet
func TestAChangeIsDrawnAsAChange(t *testing.T) {
	withColour(t)
	rows := diffLines(t, editResult, 80)
	var removed, added, context, hunk string
	for _, row := range rows {
		switch {
		case strings.Contains(stripCodes(row), "@@") && strings.Contains(row, codeOf(styleDim)):
			hunk = row
		case strings.Contains(stripCodes(row), "if line ==") && strings.Contains(row, codeOf(diffSides().del)):
			removed = row
		case strings.Contains(stripCodes(row), "TrimSpace") && strings.Contains(row, codeOf(diffSides().add)):
			added = row
		case strings.Contains(stripCodes(row), "func parse"):
			context = row
		}
	}
	for name, row := range map[string]string{"the removed line": removed, "the added line": added, "the context line": context, "the hunk": hunk} {
		if row == "" {
			t.Errorf("%s is not drawn as such:\n%s", name, joinRows(rows))
			continue
		}
		if !strings.HasPrefix(stripCodes(row), "  ") {
			t.Errorf("%s does not sit under the call row: %q", name, stripCodes(row))
		}
	}
}

// an added or removed line sits on a low ground and keeps the terminal's own words: the whole line
// painted red or green is noise, and nothing on it reads
func TestADiffLineSitsOnAGroundAndKeepsItsWords(t *testing.T) {
	withColour(t)
	rows := diffLines(t, editResult, 80)
	var removed, added string
	for _, row := range rows {
		switch {
		case strings.Contains(stripCodes(row), "TrimSpace"):
			added = row
		case strings.Contains(stripCodes(row), "if line =="):
			removed = row
		}
	}
	for name, row := range map[string]string{"a removed line": removed, "an added line": added} {
		if row == "" {
			t.Fatalf("%s is missing", name)
		}
		if !strings.Contains(row, codeOf(diffSides().del)) && !strings.Contains(row, codeOf(diffSides().add)) {
			t.Errorf("%s does not sit on a ground of its own: %q", name, stripCodes(row))
		}
		if strings.Contains(row, "\x1b[7m") {
			t.Errorf("%s is reversed out, which reads as a block over the words: %q", name, stripCodes(row))
		}
		// only the mark at the front of the row carries a colour of its own; the words are the
		// terminal's, so they stay readable on any theme
		_, words, _ := strings.Cut(row, "\x1b[0m")
		if strings.Contains(words, "38;2;") {
			t.Errorf("%s paints its words in a colour: %q", name, stripCodes(words))
		}
	}
}

// a line that changed says which part of it changed: the new words are set apart, the ones that
// stayed are not, and neither side is a solid block
func TestAChangedWordIsSetApart(t *testing.T) {
	withColour(t)
	rows := diffLines(t, editResult, 80)
	var added string
	for _, row := range rows {
		if strings.Contains(stripCodes(row), "TrimSpace") {
			added = row
		}
	}
	if added == "" {
		t.Fatal("the added line is missing")
	}
	mark := codeOf(diffSides().addMark)
	if !strings.Contains(added, mark) {
		t.Fatalf("nothing is set apart as what changed: %q", added)
	}
	at := strings.Index(added, mark)
	if before := added[:at]; strings.Contains(before, "38;2;") {
		t.Errorf("the words that stayed are marked as changed: %q", stripCodes(before))
	}
	if !strings.Contains(added[:at], codeOf(diffSides().add)) {
		t.Errorf("the line that changed does not sit on its ground: %q", stripCodes(added))
	}
	if strings.Contains(added, "\x1b[7m") {
		t.Errorf("the change is marked by reversing the words out: %q", stripCodes(added))
	}
}

// the ground of a changed line runs to the edge of the screen: a band that stops where the words do
// reads as a mistake in the drawing
func TestAChangedRowFillsItsGround(t *testing.T) {
	withColour(t)
	for _, row := range diffLines(t, editResult, 80) {
		text := stripCodes(row)
		if !strings.HasPrefix(text, "  -") && !strings.HasPrefix(text, "  +") {
			continue
		}
		if got := lipgloss.Width(text); got != 80 {
			t.Errorf("a changed row fills %d columns of an 80 column screen: %q", got, text)
		}
	}
}

// a failure's own note is not repeated under the row that already says how the command left
func TestAFailureTailLeavesOutItsOwnNote(t *testing.T) {
	var log transcript
	args := `{"command":"make"}`
	log.toolStart("exec_command", args)
	log.toolEnd("exec_command", args, "cc: segfault\nmake: *** [all] Error 1\n[exit status 2]")
	under := strings.Join(log.rows(80)[1:], "\n")
	if strings.Contains(under, "[exit status 2]") {
		t.Errorf("the tail repeats what the row says:\n%s", stripCodes(under))
	}
	if !strings.Contains(under, "Error 1") {
		t.Errorf("the tail lost the line that explains it:\n%s", stripCodes(under))
	}
}

// a command that left badly is not painted as one that succeeded, and what it printed last is what
// explains it
func TestACommandThatLeftBadlyReadsAsOne(t *testing.T) {
	withColour(t)
	var log transcript
	args := `{"command":"python3 toy_llm/train.py"}`
	log.toolStart("exec_command", args)
	log.toolEnd("exec_command", args, "Traceback\nTypeError: bad\n[exit status 1]")
	row := callRow(&log, 0)
	if !strings.Contains(row, codeOf(surfaces().fail)) {
		t.Errorf("a command that left 1 sits in the ground of a success: %q", stripCodes(row))
	}
	if strings.Contains(row, codeOf(surfaces().done)) {
		t.Errorf("a command that left 1 sits in the ground of a success: %q", stripCodes(row))
	}
}

// a long change does not bury the session: the row says how much
func TestALongChangeSaysHowMuchItLeftOut(t *testing.T) {
	var b strings.Builder
	b.WriteString("wrote big.go (4000 bytes)\n@@ -1,1 +1,40 @@\n")
	for i := 0; i < 40; i++ {
		b.WriteString("+line " + string(rune('a'+i%26)) + " of the new file\n")
	}
	rows := diffLines(t, b.String(), 80)
	if len(rows) != diffShown+1 {
		t.Fatalf("%d rows drawn, want %d and a note", len(rows), diffShown)
	}
	last := stripCodes(rows[len(rows)-1])
	if !strings.Contains(last, "more lines of the change") || !strings.Contains(last, "27") {
		t.Errorf("the note does not say what is left out: %q", last)
	}
}

// a diff row stops at the edge of the screen, wide characters included
func TestADiffRowStopsAtTheScreenEdge(t *testing.T) {
	result := "edited файл.go (10 -> 20 bytes)\n@@ -1,1 +1,1 @@\n+текст" +
		strings.Repeat(" кодом", 30) + "\n"
	rows := diffLines(t, result, 40)
	for _, row := range rows {
		if got := lipgloss.Width(stripCodes(row)); got > 40 {
			t.Errorf("a diff row takes %d columns: %q", got, stripCodes(row))
		}
	}
}

// a result that is not a change is left alone
func TestAnAnswerThatIsNotAChangeIsNotDrawnAsOne(t *testing.T) {
	if rows := changeRows("", 0, 80); rows != nil {
		t.Errorf("a plain result was drawn as a diff: %v", rows)
	}
	if holdsDiff("read src/a.go (150 lines)") {
		t.Error("a plain result reads as a change")
	}
	if !holdsDiff(editResult) {
		t.Error("an edit's change is not recognised")
	}
}

func joinRows(rows []string) string {
	var plain []string
	for _, row := range rows {
		plain = append(plain, stripCodes(row))
	}
	return strings.Join(plain, "\n")
}
