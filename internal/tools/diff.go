package tools

import (
	"fmt"
	"strings"

	"github.com/aymanbagabas/go-udiff"
)

const (
	diffMaxLines = 200  // a bigger change than this is summarised, not printed
	diffMaxBytes = 4000 // the diff is only part of a tool result
)

// diffText renders a unified diff of a change, "" if nothing changed.
func diffText(oldText, newText string) string {
	if oldText == newText {
		return ""
	}
	a, b := diffSplit(oldText), diffSplit(newText)
	oldChanged, newChanged := changedSpan(a, b)
	if oldChanged > diffMaxLines || newChanged > diffMaxLines {
		return fmt.Sprintf("[large change: %d lines replaced by %d lines; read_file to check the result]", oldChanged, newChanged)
	}
	diff := udiff.Unified("before", "after", joinLines(a), joinLines(b))
	return capDiff(dropFileHeaders(diff))
}

// changedSpan measures the middle that differs on each side, ignoring the identical band
// on both ends. That middle, not the whole file, is what makes a diff worth printing.
func changedSpan(a, b []string) (old, new int) {
	lo := 0
	for lo < len(a) && lo < len(b) && a[lo] == b[lo] {
		lo++
	}
	hi := 0
	for hi < len(a)-lo && hi < len(b)-lo && a[len(a)-1-hi] == b[len(b)-1-hi] {
		hi++
	}
	return len(a) - lo - hi, len(b) - lo - hi
}

func diffSplit(text string) []string {
	lines := strings.Split(text, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

// joinLines puts split lines back the way a diff tool expects them, with the last newline kept.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// dropFileHeaders removes the "--- before" / "+++ after" pair: a tool result already says
// which file it is about, and two lines of that per edit is tokens for nothing.
func dropFileHeaders(diff string) string {
	lines := strings.Split(diff, "\n")
	for len(lines) > 0 && (strings.HasPrefix(lines[0], "---") || strings.HasPrefix(lines[0], "+++")) {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

func capDiff(diff string) string {
	if len(diff) <= diffMaxBytes {
		return diff
	}
	cut := strings.LastIndexByte(diff[:diffMaxBytes], '\n')
	return diff[:cut] + "\n[diff cut at " + fmt.Sprint(diffMaxBytes) + " bytes; read_file to see the whole result]"
}
