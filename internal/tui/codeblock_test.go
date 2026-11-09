package tui

import (
	"strings"
	"testing"
)

// a block of code is read and copied as the code itself: nothing ykode drew may come along with a
// selection, so the block is set in from the margin rather than ruled down its side
func TestACodeBlockCarriesNothingTheReaderDidNotWrite(t *testing.T) {
	got := stripCodes(markdown("```go\nfunc main() {\n\tcount := 3\n}\n```", 60, nil))
	for _, rule := range []string{"│", "┊", "┃", "▏", "▎"} {
		if strings.Contains(got, rule) {
			t.Errorf("a code block drew %q, which a copy would carry:\n%s", rule, got)
		}
	}
	for _, want := range []string{"    func main() {", "\tcount := 3", "    }"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from:\n%s", want, got)
		}
	}
}

// colouring a block must not rewrite it: a block indented with tabs copies back as the same tabs,
// whether or not a grammar covered its language
func TestAColouredBlockKeepsTheIndentTheWriterUsed(t *testing.T) {
	withColour(t)
	code := "func main() {\n\tcount := 3\n}\n```\n"
	coloured := stripCodes(markdown("```go\n"+code, 60, nil))
	plain := stripCodes(markdown("```no_grammar_here\n"+code, 60, nil))
	if !strings.Contains(markdown("```go\n"+code, 60, nil), codeOf(styleCodeKeyword)) {
		t.Fatalf("the block under test was not coloured at all:\n%s", coloured)
	}
	if !strings.Contains(coloured, "\tcount := 3") {
		t.Errorf("a coloured block lost its tab:\n%q", coloured)
	}
	if indentOf(coloured, "count") != indentOf(plain, "count") {
		t.Errorf("the same block is indented differently coloured and plain:\ncoloured %q\nplain    %q", coloured, plain)
	}
}

// indentOf is one row of a drawn block exactly as the screen holds it, so two drawings of the same
// code can be compared on the row that matters rather than on the caption above it
func indentOf(drawn, word string) string {
	for _, row := range strings.Split(drawn, "\n") {
		if strings.Contains(row, word) {
			return row
		}
	}
	return ""
}
