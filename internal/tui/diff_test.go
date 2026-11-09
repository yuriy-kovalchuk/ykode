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
		case strings.Contains(stripCodes(row), "if line ==") && strings.Contains(row, codeOf(styleDiffDel)):
			removed = row
		case strings.Contains(stripCodes(row), "TrimSpace") && strings.Contains(row, codeOf(styleDiffAdd)):
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

// a line that changed says which part of it changed: only the new words are reversed out
func TestAChangedWordIsReversedOut(t *testing.T) {
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
	if !strings.Contains(added, "\x1b[7m") {
		t.Fatalf("nothing is marked as what changed: %q", added)
	}
	if strings.Contains(reverseRuns(added), "if line ==") {
		t.Errorf("the part that stayed the same is marked as changed: %q", stripCodes(added))
	}
	if !strings.Contains(reverseRuns(added), "TrimSpace") {
		t.Errorf("the part that changed is not marked: %q", stripCodes(added))
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

// reverseRuns collects what sits between the reverse-video marks
func reverseRuns(row string) string {
	var kept strings.Builder
	for _, part := range strings.Split(row, "\x1b[7m")[1:] {
		cut, _, _ := strings.Cut(part, "\x1b[27m")
		kept.WriteString(cut)
	}
	return kept.String()
}
