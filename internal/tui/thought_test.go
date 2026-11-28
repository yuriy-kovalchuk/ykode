package tui

import (
	"fmt"
	"strings"
	"testing"
)

// code in thinking keeps its own lines and indent, where prose wrapping would run it into one paragraph
func TestCodeInThinkingKeepsItsLinesAndIndent(t *testing.T) {
	var log transcript
	log.thinking("main.go:\n```go\nif err != nil {\n\tif tracker != nil && ctx.Err() == nil {\n\t\terr = errors.Join(err, x)\n\t}\n}\n```\n\nthat is the caller.")
	view := stripCodes(log.render(100))
	for _, want := range []string{
		"│ main.go:",
		"│     go",
		"│     if err != nil {",
		"│        if tracker != nil && ctx.Err() == nil {",
		"│           err = errors.Join(err, x)",
		"│ that is the caller.",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("thinking lacks the row %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "```") {
		t.Errorf("a fence was drawn as backticks:\n%s", view)
	}
}

// a fence still arriving is code from its first line, and a half-written closing fence never flickers
func TestAnOpenFenceInThinkingIsCodeAsItArrives(t *testing.T) {
	var log transcript
	log.thinking("try:\n```\n  go vet ./...\n``")
	view := stripCodes(log.render(80))
	if !strings.Contains(view, "│       go vet ./...") || strings.Contains(view, "``") {
		t.Errorf("an open fence is not drawn as code:\n%s", view)
	}
}

// a code span that starts a line is prose, not a fence, so the thinking around it is not cut apart
func TestACodeSpanIsNotAFence(t *testing.T) {
	parts := thoughtParts("```errors.Join``` is the call\nand more prose")
	if len(parts) != 1 || parts[0].code {
		t.Errorf("a code span opened a block: %+v", parts)
	}
}

// a long code line is cut at the screen's edge, never wrapped, so the rows below keep their columns
func TestALongCodeLineInThinkingIsCutNotWrapped(t *testing.T) {
	var log transcript
	log.thinking("```\n" + strings.Repeat("x", 300) + "\nshort\n```")
	rows := strings.Split(stripCodes(log.render(60)), "\n")
	for _, row := range rows {
		if len([]rune(row)) > 60 {
			t.Errorf("a row is %d wide on a 60 column screen: %q", len([]rune(row)), row)
		}
	}
	if !strings.Contains(strings.Join(rows, "\n"), "│     short") {
		t.Errorf("the line after the long one moved:\n%s", strings.Join(rows, "\n"))
	}
}

// aLongThought is thinking that reads, reasons and sketches code, the kind a hard step runs to
func aLongThought(blocks int) string {
	var b strings.Builder
	for i := 0; i < blocks; i++ {
		b.WriteString(strings.Repeat("considering how the runner joins its errors and what the caller reads. ", 12))
		b.WriteString("\n\nmain.go:\n```go\nresults, repoErrs, err := runner.Run(ctx)\nif err != nil {\n\tif tracker != nil {\n\t\terr = errors.Join(err, tracker.failRest(names, err))\n\t}\n\treturn fmt.Errorf(\"scan: %w\", err)\n}\n```\n\n")
	}
	return b.String()
}

// BenchmarkAThoughtStreamed is a long thought arriving a few bytes a token, drawn every few tokens
func BenchmarkAThoughtStreamed(b *testing.B) {
	text := aLongThought(30)
	b.Run(fmt.Sprintf("%dKB", len(text)/1024), func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var th thought
			for at, tokens := 0, 0; at < len(text); at, tokens = at+4, tokens+1 {
				if tokens%4 == 0 {
					_ = th.draw(text[:at+4], 110, thinkingRow)
				}
			}
		}
	})
}

// the kept rows are what a fresh draw gives at every point of the stream, fences opening and closing
func TestAThoughtStreamedDrawsAsItDoesWhole(t *testing.T) {
	text := aLongThought(3) + "```errors.Join``` is a span\n~~~\nstill open"
	var streamed thought
	for at := 1; at <= len(text); at++ {
		got := strings.Join(streamed.draw(text[:at], 90, thinkingRow), "\n")
		if want := strings.Join((&thought{}).draw(text[:at], 90, thinkingRow), "\n"); got != want {
			t.Fatalf("at byte %d the stream drew\n%s\nwhere a fresh draw gives\n%s", at, stripCodes(got), stripCodes(want))
		}
	}
}
