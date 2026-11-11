package tui

import (
	"fmt"
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

// a change is drawn as a review tool draws one: each line with its number in the file, the side that
// went on one ground and the side that came on another, each running to the edge of the screen
func TestAChangeIsNumberedAndSitsOnItsSidesGround(t *testing.T) {
	withColour(t)
	rows := diffLines(t, "edited a.go\n@@ -8,3 +8,3 @@\n ctx\n-timeout=500, rate=0.001\n+timeout=100, rate=0.001\n last\n", 80)
	plain := strings.Split(stripCodes(strings.Join(rows, "\n")), "\n")
	for i, want := range []string{"     8   ctx", "     9 - timeout=500, rate=0.001", "     9 + timeout=100, rate=0.001", "    10   last"} {
		if strings.TrimRight(plain[i], " ") != want {
			t.Errorf("row %d reads %q, want %q", i, plain[i], want)
		}
	}
	sides := diffSides()
	del, add := rows[1], rows[2]
	if !strings.Contains(del, codeOf(sides.del)) || !strings.Contains(add, codeOf(sides.add)) {
		t.Fatalf("the sides do not sit on their grounds:\n%q\n%q", del, add)
	}
	for _, row := range []string{del, add} {
		if got := lipgloss.Width(stripCodes(row)); got != 80 {
			t.Errorf("a changed row takes %d of 80 columns", got)
		}
	}
	if strings.Contains(rows[0], "\x1b[48;") {
		t.Errorf("an unchanged line was given a ground: %q", rows[0])
	}
	if strings.Contains(stripCodes(strings.Join(rows, "\n")), "@@") {
		t.Error("the hunk's header is on the screen beside the numbers that replace it")
	}
}

// the words that differ between a line and the one that replaced it sit on a stronger shade, and
// only those: what the two lines share stays on the side's own ground
func TestTheWordsThatChangedStandOut(t *testing.T) {
	withColour(t)
	rows := diffLines(t, "edited a.go\n@@ -1,1 +1,1 @@\n-timeout=500, rate=0.001\n+timeout=100, rate=0.001\n", 80)
	sides := diffSides()
	if !strings.Contains(rows[0], codeOf(sides.delWord)+"500"+styleReset) {
		t.Errorf("the number that went is not marked: %q", rows[0])
	}
	if !strings.Contains(rows[1], codeOf(sides.addWord)+"100"+styleReset) {
		t.Errorf("the number that came is not marked: %q", rows[1])
	}
	// two lines with nothing in common are two lines: marking all of each would mark nothing
	rows = diffLines(t, "edited a.go\n@@ -1,1 +1,1 @@\n-return nil\n+log.Printf(\"done in %s\", took)\n", 80)
	for _, row := range rows {
		if strings.Contains(row, codeOf(sides.delWord)) || strings.Contains(row, codeOf(sides.addWord)) {
			t.Errorf("a line replaced whole was marked word by word: %q", row)
		}
	}
}

// the shared part of two lines is counted in whole words, wide characters included
func TestWhatTwoLinesShareIsCountedInWords(t *testing.T) {
	for _, c := range []struct{ a, b, wentA, cameB string }{
		{"count := 10", "count := 100", "10", "100"},
		{"call(first)", "call(first, second)", "", ", second"},
		{"имя := старое", "имя := новое", "старое", "новое"},
		{"if x {", "if x && y {", "", "&& y "},
	} {
		inA, inB := changedSpans(c.a, c.b)
		if got := c.a[inA[0]:inA[1]]; got != c.wentA {
			t.Errorf("%q -> %q: marked %q on the old line, want %q", c.a, c.b, got, c.wentA)
		}
		if got := c.b[inB[0]:inB[1]]; got != c.cameB {
			t.Errorf("%q -> %q: marked %q on the new line, want %q", c.a, c.b, got, c.cameB)
		}
	}
}

// a change in two places of a file shows where one ends and the next begins, each with its own numbers
func TestTwoHunksAreSetApart(t *testing.T) {
	rows := diffLines(t, "edited a.go\n@@ -3,1 +3,1 @@\n-a\n+b\n@@ -120,1 +120,1 @@\n-c\n+d\n", 60)
	plain := stripCodes(strings.Join(rows, "\n"))
	if !strings.Contains(plain, diffGap) || !strings.Contains(plain, "  3 - a") || !strings.Contains(plain, "120 + d") {
		t.Errorf("the two hunks are not apart, or lost their numbers:\n%s", plain)
	}
}

// an edit is asked about as the change it would make, before the file is touched
func TestAnEditIsAskedAboutAsAChange(t *testing.T) {
	change := askedChange("edit_file", `{"path":"a.go","edits":[{"old_text":"x := 1\nreturn","new_text":"x := 2\nreturn"}]}`)
	plain := stripCodes(strings.Join(changeRows(change, 0, 60), "\n"))
	for _, want := range []string{"- x := 1", "+ x := 2"} {
		if !strings.Contains(plain, want) {
			t.Errorf("%q missing from the question:\n%s", want, plain)
		}
	}
	if askedChange("write_file", `{"path":"a.go","content":"x"}`) != "" {
		t.Error("a call that is not an edit was read as one")
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

// a change is shown whole: nothing of it is hidden behind a count
func TestALongChangeIsShownWhole(t *testing.T) {
	var b strings.Builder
	b.WriteString("wrote big.go (4000 bytes)\n@@ -1,1 +1,400 @@\n")
	for i := 0; i < 400; i++ {
		b.WriteString("+line " + string(rune('a'+i%26)) + " of the new file\n")
	}
	rows := diffLines(t, b.String(), 80)
	if len(rows) != 400 {
		t.Fatalf("%d rows drawn for a change of 400 lines", len(rows))
	}
	if plain := stripCodes(strings.Join(rows, "\n")); strings.Contains(plain, "more lines of the change") {
		t.Error("part of the change is hidden behind a count")
	}
}

// the question above the prompt has little room: it shows the start of an edit and counts the rest
func TestAQuestionShowsTheStartOfALongChange(t *testing.T) {
	change := strings.Repeat("+a line of the edit\n", 40)
	rows := changeRows(change, askChange, 80)
	if len(rows) != askChange+1 || !strings.Contains(stripCodes(rows[askChange]), "24 more lines") {
		t.Errorf("%d rows, the last %q", len(rows), stripCodes(rows[len(rows)-1]))
	}
}

// what a tool says it left out of a change is a note under it, not a line of the file
func TestANoteTheToolLeftIsNotALineOfTheChange(t *testing.T) {
	rows := diffLines(t, "wrote a.go\n@@ -1,1 +1,1 @@\n-a\n+b\n[… 12 more lines of the change; the file on disk has all of it]\n", 80)
	last := rows[len(rows)-1]
	if strings.Contains(last, "\x1b[48;") || !strings.Contains(stripCodes(last), "12 more lines") {
		t.Errorf("the note reads as a changed line: %q", last)
	}
}

// BenchmarkALongChangeDrawn is what a change of three thousand lines costs to draw, which happens
// once when it arrives and again when the terminal changes width.
func BenchmarkALongChangeDrawn(b *testing.B) {
	var change strings.Builder
	change.WriteString("@@ -1,1500 +1,1500 @@\n")
	for i := 0; i < 1500; i++ {
		fmt.Fprintf(&change, "-\tresult := compute(input, %d) // the old way\n+\tresult := compute(input, %d, options) // the new way\n", i, i)
	}
	b.ReportAllocs()
	for b.Loop() {
		changeRows(change.String(), 0, 120)
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
