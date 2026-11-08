package tools

import (
	"fmt"
	"strings"
)

const (
	// defaultResultBytes caps one tool result when the context window is unknown
	defaultResultBytes = 12000
	// tools aim under their cap so their own note about the cut still fits
	budgetSlack = 512
	noOutput    = "(no output)"
	// markerOverhead leaves room for the "bytes omitted" line itself, which fits a 10 digit count
	markerOverhead = 64
)

// ResultBytesFor scales one tool result with the window it shares: about a tenth of it in tokens,
// clamped so a small window still gets a usable result and a huge one does not drown the model.
func ResultBytesFor(ctxTokens int) int {
	if ctxTokens <= 0 {
		return defaultResultBytes
	}
	return clamp(ctxTokens/10, 1200, 6000) * 4 // four bytes per token is a generous estimate
}

// FormatResult keeps a result non-empty and inside maxBytes, keeping the head and tail of anything longer.
func FormatResult(out string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = defaultResultBytes
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return noOutput
	}
	if len(out) <= maxBytes {
		return out
	}
	// the marker costs bytes too, or the result would break the cap it promises
	keep := (maxBytes - markerOverhead) / 2
	head, tail := cutUTF8(out, keep), cutUTF8Tail(out, keep)
	return fmt.Sprintf("%s\n\n[... %d bytes omitted ...]\n\n%s", head, len(out)-len(head)-len(tail), tail)
}

// cutUTF8 returns the first n bytes of s, never splitting a rune.
func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// cutUTF8Tail returns the last n bytes of s, never splitting a rune.
func cutUTF8Tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !isRuneStart(s[start]) {
		start++
	}
	return s[start:]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// clamp keeps a model-supplied count inside a supported range.
func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }

// clampLimit maps an unset or bogus limit to the default and cuts anything over the maximum.
func clampLimit(limit, def, upper int) int {
	if limit <= 0 {
		return def
	}
	return clamp(limit, 1, upper)
}
