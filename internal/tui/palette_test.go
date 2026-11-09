package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// the terminal decides whether colours are emitted at all, so a test that checks what is coloured
// has to ask for them.
func withColour(t *testing.T) {
	t.Helper()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
}

// codeOf is the escape a style opens its text with.
func codeOf(style lipgloss.Style) string {
	open, _, _ := strings.Cut(style.Render("x"), "x")
	return open
}

// stripCodes reads what a row says without its styles. A code ends at its final byte (@ to ~), not
// at the next "m": \x1b[?25l has none.
func stripCodes(s string) string {
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return s
		}
		j := i + 2
		for j < len(s) && (s[j] < '@' || s[j] > '~') {
			j++
		}
		if j >= len(s) {
			return s[:i]
		}
		s = s[:i] + s[j+1:]
	}
}
