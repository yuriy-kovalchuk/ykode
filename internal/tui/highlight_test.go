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

// a block written in prose or in a language nobody has a grammar for stays exactly as it came:
// guessing at a language colours a sentence
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

// what the model thought is shown as it thought it: a fence inside it is code, but the screen's
// colouring of code belongs to the answer, where a reader goes to see a shape
func TestThinkingIsNeverColoured(t *testing.T) {
	withColour(t)
	var log transcript
	log.thinking("the fence is:\n```go\nfunc main() {}\n```\n")
	if strings.Contains(log.render(70), codeOf(styleCodeKeyword)) {
		t.Errorf("thinking was coloured:\n%s", stripCodes(log.render(70)))
	}
}

// a fence that has not been shut yet is on its way: colouring it again on every token would cost more
// than the rest of the frame, and it is not finished to be read as
func TestAFenceStillArrivingIsNotColoured(t *testing.T) {
	withColour(t)
	var log transcript
	log.answer("```go\nfunc main() {")
	if strings.Contains(log.render(70), codeOf(styleCodeKeyword)) {
		t.Errorf("a fence still being written was coloured:\n%s", stripCodes(log.render(70)))
	}
	// the same block, once its closing line has arrived, is coloured
	log.answer("\n```")
	if !strings.Contains(log.render(70), codeOf(styleCodeKeyword)) {
		t.Errorf("a closed fence was left plain:\n%s", stripCodes(log.render(70)))
	}
}

// a block longer than a screen full is drawn as it came: a grammar has no such limit and would pay
// for the whole thing on every frame the answer is redrawn
func TestABlockTooLongToColour(t *testing.T) {
	withColour(t)
	block := "```go\n" + strings.Repeat("func pad() int { return 1 }\n", highlightMaxBytes/26+2) + "```\n"
	var log transcript
	log.answer(block)
	if strings.Contains(log.render(70), codeOf(styleCodeKeyword)) {
		t.Error("a block over the limit was coloured")
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

// an answer is drawn again on every token that arrives, so its blocks are coloured once and asked
// for by name after that
func TestEachBlockOfAnAnswerIsColouredOnce(t *testing.T) {
	var blocks highlightCache
	block := "func main() {}"
	blocks.keep(block, "go", []string{"first"})
	if got := blocks.rowsFor(block, "go"); len(got) != 1 {
		t.Fatalf("the block was not kept: %v", got)
	}
	// the same words asked for as another language are another question
	if got := blocks.rowsFor(block, "python"); got != nil {
		t.Errorf("another language was handed the go answer: %v", got)
	}
	// more blocks than an answer holds start the cache over rather than growing it
	for i := 0; i < cacheMaxBlocks+2; i++ {
		blocks.keep(strings.Repeat("x", i), "go", []string{"row"})
	}
	if len(blocks.blocks) > cacheMaxBlocks {
		t.Errorf("the cache holds %d blocks, want it to have started over", len(blocks.blocks))
	}
	if blocks.rowsFor(block, "go") != nil {
		t.Error("the first block was kept past the cache's limit")
	}
	// nothing is asked of an answer that has no blocks, and nothing is kept for a block that was not
	// coloured: both are the plain path
	var empty highlightCache
	if empty.rowsFor("x", "go") != nil {
		t.Error("an empty cache answered")
	}
	empty.keep("x", "go", nil)
	if len(empty.blocks) != 0 {
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
	if len(entry.blocks.blocks) == 0 {
		t.Error("the answer kept no coloured block, so every frame colours again")
	}
}

// a change the reader is shown is not an answer: the code of a call keeps the colours of a diff, and
// its own rules about which lines to show
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
