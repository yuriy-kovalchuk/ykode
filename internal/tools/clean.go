package tools

import (
	"fmt"
	"regexp"
	"strings"
)

// ansiRe matches escape sequences and operating-system commands a terminal would consume.
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]|\x1b\\][^\x07]*\x07")

// rewriteRe matches a progress line that rewrote itself with carriage returns.
var rewriteRe = regexp.MustCompile("[^\n]*\r")

// cleanOutput strips what a terminal swallows and keeps the last frame of a progress line
func cleanOutput(out string) string {
	out = ansiRe.ReplaceAllString(out, "")
	out = rewriteRe.ReplaceAllStringFunc(out, func(frame string) string {
		if i := strings.LastIndexByte(frame, '\r'); i >= 0 {
			return frame[i+1:]
		}
		return frame
	})
	return collapseRepeats(out)
}

// collapseRepeats replaces a run of identical lines with the line plus how often it repeated.
func collapseRepeats(out string) string {
	lines := strings.Split(out, "\n")
	var kept []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		run := 1
		for i+run < len(lines) && lines[i+run] == line && line != "" {
			run++
		}
		kept = append(kept, line)
		if run > 2 {
			kept = append(kept, fmt.Sprintf("[previous line repeated %d more times]", run-1))
		}
		i += run - 1
	}
	return strings.Join(kept, "\n")
}
