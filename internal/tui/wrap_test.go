package tui

import (
	"strings"
	"testing"
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
