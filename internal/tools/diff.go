package tools

import (
	"fmt"
	"strings"
)

const (
	diffContext  = 3    // unchanged lines shown around a change
	diffMaxLines = 200  // a bigger change than this is summarised, not printed
	diffMaxBytes = 4000 // the diff is only part of a tool result
)

// diffText renders a unified diff of a change, "" if nothing changed.
func diffText(oldText, newText string) string {
	if oldText == newText {
		return ""
	}
	a, b := diffSplit(oldText), diffSplit(newText)
	lo := 0
	for lo < len(a) && lo < len(b) && a[lo] == b[lo] {
		lo++
	}
	hi := 0
	for hi < len(a)-lo && hi < len(b)-lo && a[len(a)-1-hi] == b[len(b)-1-hi] {
		hi++
	}
	if len(a)-lo-hi > diffMaxLines || len(b)-lo-hi > diffMaxLines {
		return fmt.Sprintf("[large change: %d lines replaced by %d lines; read_file to check the result]", len(a)-lo-hi, len(b)-lo-hi)
	}
	// keep a little of the identical band on both sides so the change has context
	start := max(0, lo-diffContext)
	am := a[start:min(len(a), len(a)-hi+diffContext)]
	bm := b[start:min(len(b), len(b)-hi+diffContext)]
	return capDiff(renderHunks(editScript(am, bm, start)))
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

type op struct {
	kind    byte // ' ' unchanged, '-' removed, '+' added
	text    string
	oldLine int
	newLine int
}

// editScript aligns the two sides with an LCS table over the changed middle.
func editScript(a, b []string, offset int) []op {
	n, m := len(a), len(b)
	table := make([][]uint32, n+1)
	for i := range table {
		table[i] = make([]uint32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else {
				table[i][j] = max(table[i+1][j], table[i][j+1])
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{' ', a[i], offset + i + 1, offset + j + 1})
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			ops = append(ops, op{'-', a[i], offset + i + 1, 0})
			i++
		default:
			ops = append(ops, op{'+', b[j], 0, offset + j + 1})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{'-', a[i], offset + i + 1, 0})
	}
	for ; j < m; j++ {
		ops = append(ops, op{'+', b[j], 0, offset + j + 1})
	}
	return ops
}

func renderHunks(ops []op) string {
	var sb strings.Builder
	for _, hunk := range hunkRanges(ops) {
		oldStart, oldCount, newStart, newCount := 0, 0, 0, 0
		for _, o := range ops[hunk.start:hunk.end] {
			if o.oldLine > 0 {
				if oldCount == 0 {
					oldStart = o.oldLine
				}
				oldCount++
			}
			if o.newLine > 0 {
				if newCount == 0 {
					newStart = o.newLine
				}
				newCount++
			}
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		for _, o := range ops[hunk.start:hunk.end] {
			sb.WriteByte(o.kind)
			sb.WriteString(o.text)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

type hunk struct{ start, end int }

func hunkRanges(ops []op) []hunk {
	var out []hunk
	lastChange, open := -1, -1
	for i, o := range ops {
		if o.kind == ' ' {
			if open >= 0 && i-lastChange > 2*diffContext {
				out = append(out, hunk{open, min(i+diffContext, len(ops))})
				open = -1
			}
			continue
		}
		if open < 0 {
			open = max(0, i-diffContext)
		}
		lastChange = i + 1
	}
	if open >= 0 {
		out = append(out, hunk{open, min(lastChange+diffContext, len(ops))})
	}
	return out
}

func capDiff(diff string) string {
	if len(diff) <= diffMaxBytes {
		return diff
	}
	cut := strings.LastIndexByte(diff[:diffMaxBytes], '\n')
	return diff[:cut] + "\n[diff cut at " + fmt.Sprint(diffMaxBytes) + " bytes; read_file to see the whole result]"
}
