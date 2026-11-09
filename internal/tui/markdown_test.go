package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

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
	got := stripCodes(markdown(answer, 60, nil))
	for _, want := range []string{
		"What I found", "three", "- the record, its rows", "1. read the file",
		"│ func main() {}", "go",
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
	got := stripCodes(markdown("looking now\n```go\nfunc main() {", 60, nil))
	if !strings.Contains(got, "│ func main() {") {
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
		for _, line := range strings.Split(markdown(long, width, nil), "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("%d-wide screen has a %d-wide row: %q", width, w, line)
			}
		}
	}
}

// a link keeps its target in words a person can follow
func TestALinkKeepsItsAddress(t *testing.T) {
	got := stripCodes(markdown("see [the docs](https://ykode.dev/tools) for more", 80, nil))
	if !strings.Contains(got, "the docs (https://ykode.dev/tools)") {
		t.Errorf("the link was not opened: %q", got)
	}
}

// text inside a code span is left exactly as the model wrote it
func TestCodeSpansKeepTheirOwnMarks(t *testing.T) {
	got := stripCodes(markdown("run `go test ./... -count=1` now", 80, nil))
	if !strings.Contains(got, "go test ./... -count=1") {
		t.Errorf("the command inside the code span changed: %q", got)
	}
}

// a list inside a list keeps its shape, which a line-at-a-time guess could not see
func TestANestedListKeepsItsShape(t *testing.T) {
	got := stripCodes(markdown("steps:\n- one\n  - nested under one\n- two\n", 60, nil))
	if !strings.Contains(got, "  - one") || !strings.Contains(got, "      - nested under one") {
		t.Errorf("the nested entry is not under its parent:\n%s", got)
	}
}

// a table the model drew stays readable rather than turning into a wall of dashes
func TestATableStaysReadable(t *testing.T) {
	got := stripCodes(markdown("| file | lines |\n|---|---|\n| rows.go | 140 |\n", 60, nil))
	if !strings.Contains(got, "file · lines") || !strings.Contains(got, "rows.go · 140") {
		t.Errorf("the table is not on rows of its own:\n%s", got)
	}
	if strings.Contains(got, "|---|") {
		t.Errorf("the table's rules are left on the screen:\n%s", got)
	}
}

// a mark the model escaped stays in the text rather than vanishing
func TestAMarkTheModelEscapedStaysPut(t *testing.T) {
	got := stripCodes(markdown(`write \*count\* in the file`, 80, nil))
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
		markdown(answer, 100, nil)
	}
}
