package tui

import (
	"regexp"
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

// a change is drawn as a change: one low ground per side, a glyph in the side's colour, and no second,
// stronger shade inside a line
func TestAChangeHasOneGroundPerSide(t *testing.T) {
	withColour(t)
	rows := diffLines(t, "edited a.go\n@@ -1,2 +1,2 @@\n ctx\n-timeout=500, rate=0.001\n+timeout=100, rate=0.001\n", 80)
	var del, add string
	for _, row := range rows {
		switch plain := strings.TrimSpace(stripCodes(row)); {
		case strings.HasPrefix(plain, "-"):
			del = row
		case strings.HasPrefix(plain, "+"):
			add = row
		}
	}
	sides := diffSides()
	if !strings.Contains(del, codeOf(sides.del)) || !strings.Contains(add, codeOf(sides.add)) {
		t.Fatalf("the sides do not sit on their grounds:\n%q\n%q", del, add)
	}
	if codeOf(sides.add) == codeOf(sides.del) {
		t.Error("both sides share one ground")
	}
	for _, row := range []string{del, add} {
		if n := len(regexp.MustCompile(`\x1b\[48;[0-9;]*m`).FindAllString(row, -1)); n == 0 {
			t.Errorf("a changed row has no ground: %q", row)
		}
		grounds := map[string]bool{}
		for _, g := range regexp.MustCompile(`\x1b\[48;[0-9;]*m`).FindAllString(row, -1) {
			grounds[g] = true
		}
		if len(grounds) != 1 {
			t.Errorf("a changed row uses %d shades, want the one of its side: %q", len(grounds), row)
		}
		if got := lipgloss.Width(stripCodes(row)); got != 80 {
			t.Errorf("a changed row takes %d of 80 columns", got)
		}
	}
	if !strings.Contains(del, codeOf(styleDelMark)) || !strings.Contains(add, codeOf(styleAddMark)) {
		t.Error("the glyphs are not coloured by side")
	}
}

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

// a command that left badly is not marked as one that succeeded, and what it printed last is what
// explains it
func TestACommandThatLeftBadlyReadsAsOne(t *testing.T) {
	var log transcript
	args := `{"command":"python3 toy_llm/train.py"}`
	log.toolStart("exec_command", args)
	log.toolEnd("exec_command", args, "Traceback\nTypeError: bad\n[exit status 1]")
	row := stripCodes(callRow(&log, 0))
	if !strings.Contains(row, "✗") || strings.Contains(row, "✓") {
		t.Errorf("a command that left 1 is marked as a success: %q", row)
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
