package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// the styles a code block is coloured with, so a test can ask whether any of them was used
func codeStyles() []lipgloss.Style {
	return []lipgloss.Style{styleCodeKeyword, styleCodeString, styleCodeName, styleCodeComment, styleAddMark, styleDelMark}
}

// an answer that names a language for its fence gets that language's shapes in colour
func TestAFenceNamingALanguageIsColoured(t *testing.T) {
	withColour(t)
	var log transcript
	log.answer("the change:\n\n```go\nfunc main() {\n\tcount := \"three\"\n}\n```\n")
	got := log.render(70)
	for _, want := range []string{
		codeOf(styleCodeKeyword) + "func", // the word that says a thing is defined
		codeOf(styleCodeString) + "\"three\"",
		codeOf(styleCodeName) + "main", // what is called
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from:\n%s", want, stripCodes(got))
		}
	}
}

// prose and languages without a grammar stay exactly as they came: guessing colours a sentence
func TestAFenceNobodyNamedIsLeftAlone(t *testing.T) {
	withColour(t)
	for _, c := range []struct{ what, answer string }{
		{"no language", "```\n\"case one\" or \"case two\"\n```"},
		{"no grammar", "```not_a_real_language\nconst a = 1\n```"},
		{"an indented block", "    count := \"three\"\n"},
	} {
		var log transcript
		log.answer(c.answer)
		got := log.render(70)
		for _, style := range codeStyles() {
			if strings.Contains(got, codeOf(style)) {
				t.Errorf("%s was coloured:\n%s", c.what, stripCodes(got))
				break
			}
		}
	}
}

// thinking shows as it was thought: a fence in it is code, coloured code belongs to the answer
func TestThinkingIsNeverColoured(t *testing.T) {
	withColour(t)
	var log transcript
	log.thinking("the fence is:\n```go\nfunc main() {}\n```\n")
	if strings.Contains(log.render(70), codeOf(styleCodeKeyword)) {
		t.Errorf("thinking was coloured:\n%s", stripCodes(log.render(70)))
	}
}

// an open fence is coloured as it arrives, so the screen never seems to fall behind
func TestAFenceStillArrivingIsColoured(t *testing.T) {
	withColour(t)
	var log transcript
	log.answer("```go\nfunc main() {")
	if !strings.Contains(log.render(70), codeOf(styleCodeKeyword)+"func") {
		t.Errorf("a fence still being written was left plain:\n%s", stripCodes(log.render(70)))
	}
	// the closing fence arrives a backtick at a time, and is never drawn as a line of the code
	for _, piece := range []string{"\n", "`", "`", "`"} {
		log.answer(piece)
		if got := stripCodes(log.render(70)); strings.Contains(got, "`") {
			t.Errorf("the closing fence was drawn as code while it arrived:\n%s", got)
		}
	}
	if !strings.Contains(log.render(70), codeOf(styleCodeKeyword)+"func") {
		t.Errorf("a closed fence was left plain:\n%s", stripCodes(log.render(70)))
	}
}

// a block still arriving is read a chunk at a time: the settled lines are not read again
func TestALongBlockArrivingIsReadInChunks(t *testing.T) {
	withColour(t)
	line := "func pad() int { return 1 } // a line of the block\n"
	var cache codeCache
	code := ""
	for len(code) < 4*codeChunkBytes {
		code += line
		rows := cache.rows(strings.TrimRight(code, "\n"), "go", false)
		if want := strings.Count(code, "\n"); len(rows) != want {
			t.Fatalf("%d lines of code drew %d rows", want, len(rows))
		}
		if unread := len(code) - cache.live.settled; unread > codeChunkBytes+len(line) {
			t.Fatalf("%d bytes are read again on every frame, more than a chunk", unread)
		}
	}
	for i, row := range cache.rows(strings.TrimRight(code, "\n"), "go", false) {
		if !strings.HasPrefix(row, codeOf(styleCodeKeyword)+"func") {
			t.Fatalf("row %d lost its colour where two chunks meet: %q", i, row)
		}
	}
}

// a block longer than one reading is still coloured once it has closed, and is read once
func TestABlockOfAnyLengthIsColoured(t *testing.T) {
	withColour(t)
	block := "```go\n" + strings.Repeat("func pad() int { return 1 }\n", 2*codeExactBytes/28) + "```\n"
	var log transcript
	log.answer(block)
	rows := strings.Split(log.render(70), "\n")
	if len(rows) < 2*codeExactBytes/28 {
		t.Fatalf("the block drew %d rows", len(rows))
	}
	for _, row := range rows[1:] {
		if !strings.Contains(row, codeOf(styleCodeKeyword)+"func") {
			t.Fatalf("a row of a long block was left plain: %q", row)
		}
	}
}

// a diff block says which lines went and which came, in the same two colours the rows of a change use
func TestADiffBlockSaysWhatMoved(t *testing.T) {
	withColour(t)
	var log transcript
	log.answer("```diff\n--- a/x\n+++ b/x\n-const a = 1\n+const a = 2\n```\n")
	got := log.render(70)
	if !strings.Contains(got, codeOf(styleAddMark)) || !strings.Contains(got, codeOf(styleDelMark)) {
		t.Errorf("a diff block lost its two colours:\n%s", stripCodes(got))
	}
}

// a coloured row is still cut to the screen, and the colour does not run into the rows under it
func TestAColouredRowStaysInsideTheScreen(t *testing.T) {
	withColour(t)
	var log transcript
	log.answer("```go\n" + strings.Repeat("func aVeryLongFunctionName(argument int) int { return 1 }\n", 4) + "```\n")
	for _, row := range strings.Split(log.render(30), "\n") {
		if w := lipgloss.Width(row); w > 30 {
			t.Errorf("a coloured row is %d wide on a 30 column screen: %q", w, stripCodes(row))
		}
	}
	rows := strings.Split(log.render(30), "\n")
	if last := rows[len(rows)-1]; strings.Contains(last, "\x1b[") && !strings.HasSuffix(last, "\x1b[0m") {
		t.Errorf("a cut row left its colour open: %q", last)
	}
}

// a closed block is read once and fetched by its text, since a width change redraws every answer
func TestEachBlockOfAnAnswerIsColouredOnce(t *testing.T) {
	withColour(t)
	var blocks codeCache
	block := "func main() {}"
	first := blocks.rows(block, "go", true)
	if len(blocks.closed) != 1 {
		t.Fatalf("the block was not kept: %v", blocks.closed)
	}
	if again := blocks.rows(block, "go", true); &again[0] != &first[0] {
		t.Error("the same block was read a second time")
	}
	// the same words asked for as another language are another question
	if got := blocks.rows(block, "python", true); got[0] == first[0] {
		t.Errorf("another language was handed the go answer: %q", got)
	}
	// more blocks than an answer holds start the cache over rather than growing it
	for i := 0; i < codeKeptBlocks+2; i++ {
		blocks.rows(strings.Repeat("x", i+1), "go", true)
	}
	if len(blocks.closed) > codeKeptBlocks {
		t.Errorf("the cache holds %d blocks, want it to have started over", len(blocks.closed))
	}
	// nothing is kept for a block nobody has a grammar for: it is the plain path
	var plain codeCache
	if plain.rows("x", "", true) != nil || plain.rows("x", "not_a_real_language", true) != nil {
		t.Error("a block with no grammar was coloured")
	}
	if len(plain.closed) != 0 {
		t.Error("a block left plain was kept anyway")
	}
}

// the cache belongs to one entry and goes with it
func TestAColouredAnswerKeepsItsBlocksAcrossRedraws(t *testing.T) {
	withColour(t)
	var log transcript
	log.answer("```go\nfunc main() {}\n```\n")
	first := log.render(60)
	second := log.render(60)
	if first != second {
		t.Errorf("the same answer drew differently:\n%s\n---\n%s", stripCodes(first), stripCodes(second))
	}
	entry := &log.entries[len(log.entries)-1]
	if len(entry.laid.code.closed) == 0 {
		t.Error("the answer kept no coloured block, so every frame colours again")
	}
}

// a change shown to the reader is not an answer: a call keeps the diff's colours and rules
func TestACallIsNotColouredAsAnAnswer(t *testing.T) {
	withColour(t)
	var log transcript
	log.toolStart("write_file", `{"path":"x.go"}`)
	log.toolEnd("write_file", `{"path":"x.go"}`, "--- a/x.go\n+++ b/x.go\n-const a = 1\n+const a = 2\n")
	log.done(time.Now(), time.Second)
	if strings.Contains(log.render(70), codeOf(styleCodeKeyword)) {
		t.Errorf("a call was coloured like an answer:\n%s", stripCodes(log.render(70)))
	}
}
