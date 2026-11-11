package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// markdown is an answer laid out from nothing, as one string: what a test reads.
func markdown(source string, width int) string {
	return strings.Join((&layout{}).draw(source, width, ""), "\n")
}

// an answer the model shapes with markdown is drawn with that shape, not with the marks
func TestAnAnswerGetsItsShape(t *testing.T) {
	answer := strings.Join([]string{
		"# What I found",
		"",
		"The file has **three** parts and a `main` function.",
		"",
		"- the record, its rows",
		"- the text rules",
		"",
		"1. read the file",
		"2. change it",
		"",
		"---",
		"",
		"```go",
		"func main() {}",
		"```",
	}, "\n")
	got := stripCodes(markdown(answer, 60))
	for _, want := range []string{
		"What I found", "three", "• the record, its rows", "1. read the file",
		"    func main() {}", "go",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from:\n%s", want, got)
		}
	}
	for _, not := range []string{"# ", "**", "```", "`"} {
		if strings.Contains(got, not) {
			t.Errorf("%q is still on the screen:\n%s", not, got)
		}
	}
}

// an answer arrives a token at a time: a fence that has not closed yet is already code
func TestAFenceStillBeingWrittenIsShownAsCode(t *testing.T) {
	got := stripCodes(markdown("looking now\n```go\nfunc main() {", 60))
	if !strings.Contains(got, "    func main() {") {
		t.Errorf("the half-written fence is not drawn as code:\n%s", got)
	}
	if strings.Contains(got, "```") {
		t.Errorf("the fence marks are left on the screen:\n%s", got)
	}
}

// prose a model wrote in one long line is broken to the screen, and nothing runs past it
func TestAnAnswerRowNeverRunsPastTheScreen(t *testing.T) {
	long := strings.Repeat("a word ", 60) + "\n\n- " + strings.Repeat("another long item ", 40)
	for _, width := range []int{40, 80} {
		for _, line := range strings.Split(markdown(long, width), "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("%d-wide screen has a %d-wide row: %q", width, w, line)
			}
		}
	}
}

// a link keeps its target in words a person can follow
func TestALinkKeepsItsAddress(t *testing.T) {
	got := stripCodes(markdown("see [the docs](https://ykode.dev/tools) for more", 80))
	if !strings.Contains(got, "the docs (https://ykode.dev/tools)") {
		t.Errorf("the link was not opened: %q", got)
	}
}

// text inside a code span is left exactly as the model wrote it
func TestCodeSpansKeepTheirOwnMarks(t *testing.T) {
	got := stripCodes(markdown("run `go test ./... -count=1` now", 80))
	if !strings.Contains(got, "go test ./... -count=1") {
		t.Errorf("the command inside the code span changed: %q", got)
	}
}

// a list inside a list keeps its shape, which a line-at-a-time guess could not see
func TestANestedListKeepsItsShape(t *testing.T) {
	got := stripCodes(markdown("steps:\n- one\n  - nested under one\n- two\n", 60))
	if !strings.Contains(got, "  • one\n    ◦ nested under one\n  • two") {
		t.Errorf("the nested entry is not under its parent:\n%s", got)
	}
}

// a table the model drew is drawn in columns, each as wide as its widest cell
func TestATableIsDrawnInColumns(t *testing.T) {
	got := stripCodes(markdown("| file | lines |\n|---|---:|\n| rows.go | 140 |\n| a.go | 7 |\n", 60))
	for _, want := range []string{
		"  ┌─────────┬───────┐",
		"  │ file    │ lines │",
		"  ├─────────┼───────┤",
		"  │ rows.go │   140 │",
		"  │ a.go    │     7 │",
		"  └─────────┴───────┘",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from:\n%s", want, got)
		}
	}
	if strings.Contains(got, "|---|") {
		t.Errorf("the table's rules are left on the screen:\n%s", got)
	}
}

// a table wider than the screen keeps its grid: the widest column gives, and its cells wrap inside it
func TestAWideTableWrapsInsideItsColumns(t *testing.T) {
	got := markdown("| file | what it holds |\n|---|---|\n| rows.go | "+strings.Repeat("the rows of the session ", 6)+"|\n| a.go | little |\n", 40)
	rows := strings.Split(stripCodes(got), "\n")
	for _, row := range rows {
		if w := lipgloss.Width(row); w > 40 {
			t.Errorf("a table row is %d wide on a 40 column screen: %q", w, row)
		}
		if !strings.HasPrefix(row, "  ┌") && !strings.HasPrefix(row, "  │") && !strings.HasPrefix(row, "  ├") && !strings.HasPrefix(row, "  └") {
			t.Errorf("a row of the table left its grid: %q", row)
		}
	}
	plain := strings.Join(rows, "\n")
	if !strings.Contains(plain, "│ rows.go │ the rows of the") || strings.Count(plain, "├") != 2 {
		t.Errorf("the cells did not wrap inside their columns, a rule between the rows:\n%s", plain)
	}
}

// a table with more columns than the screen can rule is written as records, a cell a line
func TestATableTooWideForAGridIsWrittenAsRecords(t *testing.T) {
	got := stripCodes(markdown("| a | b | c | d | e | f |\n|---|---|---|---|---|---|\n| one | two | three | four | five | six |\n", 24))
	for _, want := range []string{"  a: one", "  f: six"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from:\n%s", want, got)
		}
	}
}

// a line the model broke is broken on the screen, and a <br> breaks a line inside a table's cell
func TestLinesTheModelBrokeStayBroken(t *testing.T) {
	got := stripCodes(markdown("name: rows.go\nlines: 140\nowner: tui", 60))
	if got != "name: rows.go\nlines: 140\nowner: tui" {
		t.Errorf("three lines of data were run together: %q", got)
	}
	got = stripCodes(markdown("| k | v |\n|---|---|\n| a | one<br>two |\n", 60))
	if !strings.Contains(got, "│ a │ one │\n  │   │ two │") {
		t.Errorf("the break inside the cell was lost:\n%s", got)
	}
}

// blocks stand apart: two paragraphs that run into each other read as one
func TestBlocksOfAnAnswerStandApart(t *testing.T) {
	got := markdown("the first thing.\n\nthe second thing.\n\n- a list\n", 60)
	if want := "the first thing.\n\nthe second thing.\n\n  • a list"; got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// a mark inside a mark does not switch the outer one off: bold words around a code span are bold on
// both sides of it
func TestAStyleInsideAStyleLeavesTheOuterOneOn(t *testing.T) {
	withColour(t)
	got := markdown("**before `code` after**", 60)
	bold := codeOf(styleBold)
	if strings.Count(got, bold) < 2 || !strings.Contains(got, bold+" after") {
		t.Errorf("the words after the code span are not bold: %q", got)
	}
}

// the screen draws rows on their own, so a style that runs over a break is closed on the row it
// began on and opened again on the row it continues on
func TestAStyleThatWrapsIsWholeOnEveryRow(t *testing.T) {
	withColour(t)
	rows := strings.Split(markdown("**"+strings.TrimSpace(strings.Repeat("bold words ", 12))+"**", 40), "\n")
	if len(rows) < 3 {
		t.Fatalf("the text did not wrap: %q", rows)
	}
	for _, row := range rows {
		if !strings.HasPrefix(row, codeOf(styleBold)) || !strings.HasSuffix(row, styleReset) {
			t.Errorf("a row of bold text does not open and close its own style: %q", row)
		}
	}
}

// a checklist reads as boxes and struck-out text as struck out, not as the marks that asked for them
func TestChecklistsAndStruckOutText(t *testing.T) {
	got := stripCodes(markdown("- [x] done\n- [ ] not yet\n\n~~gone~~ kept", 60))
	for _, want := range []string{"• [x] done", "• [ ] not yet", "gone kept"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from:\n%s", want, got)
		}
	}
	if strings.Contains(got, "~~") {
		t.Errorf("the strike marks are left on the screen:\n%s", got)
	}
}

// an answer still arriving is laid out a block at a time: what a frame draws from the blocks it kept
// is exactly what drawing the whole answer from nothing gives
func TestAnAnswerDrawnAsItArrivesMatchesOneDrawnWhole(t *testing.T) {
	withColour(t)
	answer := "# What I found\n\nThe file has **three** parts and a `main` function.\n\n- the record\n- the rules\n\n" +
		"```go\nfunc main() {\n\tcount := \"three\"\n}\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n> quoted\n\nthe end\n"
	var arriving layout
	for upto := 1; upto <= len(answer); upto++ {
		got := strings.Join(arriving.draw(answer[:upto], 50, "  "), "\n")
		if want := strings.Join((&layout{}).draw(answer[:upto], 50, "  "), "\n"); got != want {
			t.Fatalf("after %d bytes the two drawings differ:\narriving %q\nwhole    %q", upto, got, want)
		}
	}
	if arriving.settled == 0 {
		t.Error("no block was ever kept, so every frame lays the whole answer out again")
	}
}

// a mark the model escaped stays in the text rather than vanishing
func TestAMarkTheModelEscapedStaysPut(t *testing.T) {
	got := stripCodes(markdown(`write \*count\* in the file`, 80))
	if !strings.Contains(got, "*count*") {
		t.Errorf("the escaped marks disappeared: %q", got)
	}
}

// an answer is redrawn as it arrives, so its cost is paid once per token
func BenchmarkAnswerDrawn(b *testing.B) {
	answer := strings.Join([]string{
		"# What I found", "",
		"The file has **three** parts and a `main` function, and " + strings.Repeat("some prose to fill the row. ", 12), "",
		"- the record, its rows", "- the text rules", "- the wrapping", "",
		"```go", "func main() {", "\tprintln(\"hi\")", "}", "```",
	}, "\n")
	for i := 0; i < b.N; i++ {
		markdown(answer, 100)
	}
}
