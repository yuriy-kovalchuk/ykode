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
	// maxResultTokens is the most one result may take: 51,200 bytes at four bytes a token, pi's cap
	maxResultTokens = 12800
)

// resultCap is the one arithmetic between a result budget and what a tool may fill: the cap, the room
// a note about the cut needs, and the floor below which there is nothing worth cutting. The file tools'
// scope and the command tool each worked this out for itself, with its own copy of the same three
// numbers, and they agreed only because two pieces of arithmetic happened to point the same way.
//
// FormatResult is deliberately not on this path: it is the last gate before text reaches the model, and
// the cap it is handed is one it is expected to honour exactly rather than aim under. Two tests pin the
// difference, and the cap it is given has already been aimed under by whoever measured the window.

// minAimBytes is the least a result may hold, so a mis-set or tiny budget degrades to a short answer
// instead of to a broken cut.
const minAimBytes = 64

func resultCap(budget int) int {
	if budget <= 0 {
		budget = defaultResultBytes
	}
	return max(budget-budgetSlack, minAimBytes)
}

// ResultBytesFor scales one tool result with the window it shares: about a tenth of it in tokens,
// clamped so a small window still gets a usable result. The ceiling is pi's 50 KB, reached at a 128k
// window: a whole source file fits in one result, and the whole call goes when room runs short.
func ResultBytesFor(ctxTokens int) int {
	if ctxTokens <= 0 {
		return defaultResultBytes
	}
	return clamp(ctxTokens/10, 1200, maxResultTokens) * 4 // four bytes per token is a generous estimate
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
	// the marker costs bytes too, or the result would break the cap it promises - and a cap smaller than
	// the marker holds nothing to split, so it gets the marker alone rather than a cut of negative length
	if maxBytes < markerOverhead {
		return fmt.Sprintf("[%d bytes omitted: the result budget is too small to show any of it]", len(out))
	}
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

// oneLine trims the end of a line and cuts it when long, keeping output scannable.
func oneLine(s string, maxChars int) string {
	s = strings.TrimRight(s, " \t\r") // leading tabs stay: a line copied from here becomes edit_file's old_text
	if len(s) > maxChars {
		return cutUTF8(s, maxChars) + " […cut]"
	}
	return s
}
