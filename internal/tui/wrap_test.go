package tui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLongThinkingWrapsInsteadOfOverflowing(t *testing.T) {
	var log transcript
	log.thinking(strings.Repeat("word ", 60))
	lines := strings.Split(log.render(40, false), "\n")
	if len(lines) < 5 {
		t.Fatalf("a long block stayed on too few lines: %d", len(lines))
	}
	for _, line := range lines {
		if len(strings.TrimSpace(stripCodes(line))) > 40 {
			t.Errorf("a line is wider than the screen: %q", stripCodes(line))
		}
	}
}

// truncating a row must not split a character: Cyrillic and emoji are ordinary in tool output
func TestTruncatingARowKeepsWholeCharacters(t *testing.T) {
	row := oneLine("модель подписала", 14)
	if !utf8.ValidString(row) {
		t.Errorf("the cut split a character: %q", row)
	}
	if !strings.HasSuffix(row, "…") {
		t.Errorf("the cut is not marked: %q", row)
	}
}
