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

// changeMaxLines is the most of one change a screen is handed, far past what anyone reads per row
const changeMaxLines = 3000

// changeMaxBytes caps a change by size too, since lines are not a measure: a minified file is one line
const changeMaxBytes = 200_000

// wholeChange is a change as a person reads it: every hunk, capped in lines and bytes, "" if equal
func wholeChange(oldText, newText string) string {
	if oldText == newText {
		return ""
	}
	a, b := diffSplit(oldText), diffSplit(newText)
	var lines []string
	if went, came := changedSpan(a, b); went+came > changeMaxLines {
		// a whole-file rewrite: lining the halves up costs seconds and says nothing, so it says went, then came
		lines = blockChange(a, b, went, came)
	} else {
		diff := dropFileHeaders(udiff.Unified("before", "after", joinLines(a), joinLines(b)))
		lines = strings.Split(strings.TrimRight(diff, "\n"), "\n")
	}
	if len(lines) <= changeMaxLines {
		return cutChange(strings.Join(lines, "\n"))
	}
	return cutChange(strings.Join(lines[:changeMaxLines], "\n")) +
		fmt.Sprintf("\n[… %d more lines of the change; the file on disk has all of it]", len(lines)-changeMaxLines)
}

// cutChange takes off the bytes a line count misses, landing the cut on a line break where one is near
func cutChange(change string) string {
	if len(change) <= changeMaxBytes {
		return change
	}
	cut := change[:changeMaxBytes]
	if at := strings.LastIndexByte(cut, '\n'); at > changeMaxBytes/2 {
		cut = cut[:at]
	}
	return cut + fmt.Sprintf("\n[… %d more bytes of the change; the file on disk has all of it]", len(change)-len(cut))
}

// blockChange is the differing middle of two files as one hunk: what went, then what came
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

// changedSpan measures the middle that differs, ignoring the identical band on both ends
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

// dropFileHeaders removes "--- before"/"+++ after": the result already names the file
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
