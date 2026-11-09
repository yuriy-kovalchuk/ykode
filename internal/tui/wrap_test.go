package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
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

// what matters for a row is how much screen it takes, not how many bytes or runes it holds: a CJK
// glyph or an emoji is two columns wide
func TestARowOfWideCharactersStopsAtTheScreenEdge(t *testing.T) {
	row := oneLine("コードをかく · проверка работы · 🧪 a long argument for the tool that writes files", 30)
	if got := lipgloss.Width(stripCodes(row)); got > 30 {
		t.Errorf("the row takes %d columns, want 30 or fewer: %q", got, row)
	}
}

// a long one-line tool result is cut on every redraw, so cutting is measured once and not again
// for every character dropped.
func BenchmarkCuttingARow(b *testing.B) {
	line := strings.Repeat("x", 6000)
	for b.Loop() {
		cutToWidth(line, 60)
	}
}

// text in characters with no spaces between them still stops at the edge of the screen: it is
// broken at the column rather than kept as one word that runs off it.
func TestTextWithoutSpacesIsBrokenAtTheEdge(t *testing.T) {
	rows := wrap(strings.Repeat("你", 30), 20)
	if len(rows) != 3 {
		t.Fatalf("60 columns at 20 wide came as %d rows: %q", len(rows), rows)
	}
	for _, line := range rows {
		if got := lipgloss.Width(line); got != 20 {
			t.Errorf("a row takes %d columns: %q", got, line)
		}
	}
}

// a word wider than the screen - a long path, a URL - is broken instead of overflowing
func TestAWordWiderThanTheScreenIsBroken(t *testing.T) {
	text := "see internal/super/calibrated/very/long/path/to/file.go for details"
	for _, line := range wrap(text, 16) {
		if got := lipgloss.Width(line); got > 16 {
			t.Errorf("a row takes %d columns of a 16 column screen: %q", got, line)
		}
	}
}
