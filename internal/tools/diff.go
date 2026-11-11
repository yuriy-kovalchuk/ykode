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

// changeMaxLines is the most of one change a screen is handed. It is far past what anyone reads on a
// row-by-row screen, and it is what keeps a rewritten file of a hundred thousand lines from being
// held twice over; the model's own copy of the change is capped much lower, and separately.
const changeMaxLines = 3000

// wholeChange is a change as a person reads it, not as the model is charged for it: every hunk, with
// no cap on bytes. It is "" when nothing changed.
func wholeChange(oldText, newText string) string {
	if oldText == newText {
		return ""
	}
	a, b := diffSplit(oldText), diffSplit(newText)
	var lines []string
	if went, came := changedSpan(a, b); went+came > changeMaxLines {
		// a file rewritten from top to bottom: lining its halves up costs seconds and says nothing a
		// reader can use, so the part that differs is given as what went, then what came
		lines = blockChange(a, b, went, came)
	} else {
		diff := dropFileHeaders(udiff.Unified("before", "after", joinLines(a), joinLines(b)))
		lines = strings.Split(strings.TrimRight(diff, "\n"), "\n")
	}
	if len(lines) <= changeMaxLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:changeMaxLines], "\n") +
		fmt.Sprintf("\n[… %d more lines of the change; the file on disk has all of it]", len(lines)-changeMaxLines)
}

// blockChange is the differing middle of two files as one hunk: the lines that went, then the lines
// that came.
func blockChange(a, b []string, went, came int) []string {
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	lines := make([]string, 0, went+came+1)
	lines = append(lines, fmt.Sprintf("@@ -%d,%d +%d,%d @@", start+1, went, start+1, came))
	for _, line := range a[start : start+went] {
		lines = append(lines, "-"+line)
	}
	for _, line := range b[start : start+came] {
		lines = append(lines, "+"+line)
	}
	return lines
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
