package tui

import (
	"strings"
	"testing"
)

// a code block is read and copied as the code itself, so it is inset from the margin, not ruled
func TestACodeBlockCarriesNothingTheReaderDidNotWrite(t *testing.T) {
	got := stripCodes(markdown("```go\nfunc main() {\n\tcount := 3\n}\n```", 60))
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

// colouring must not rewrite a block: a tab-indented block copies back as the same tabs
func TestAColouredBlockKeepsTheIndentTheWriterUsed(t *testing.T) {
	code := "func main() {\n\tcount := 3\n}\n```\n"
	coloured := stripCodes(markdown("```go\n"+code, 60))
	plain := stripCodes(markdown("```no_grammar_here\n"+code, 60))
	if !strings.Contains(markdown("```go\n"+code, 60), codeOf(styleCodeKeyword)) {
		t.Fatalf("the block under test was not coloured at all:\n%s", coloured)
	}
	if !strings.Contains(coloured, "\tcount := 3") {
		t.Errorf("a coloured block lost its tab:\n%q", coloured)
	}
	if indentOf(coloured, "count") != indentOf(plain, "count") {
		t.Errorf("the same block is indented differently coloured and plain:\ncoloured %q\nplain    %q", coloured, plain)
	}
}

// indentOf is one drawn row as the screen holds it, to compare drawings on the row that matters
func indentOf(drawn, word string) string {
	for _, row := range strings.Split(drawn, "\n") {
		if strings.Contains(row, word) {
			return row
		}
	}
	return ""
}
