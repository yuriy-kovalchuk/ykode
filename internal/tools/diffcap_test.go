package tools

import (
	"strings"
	"testing"
)

// Lines are not a measure of size. A minified file is one very long line, so a change to it is the
// whole file twice over and the line cap never engages: this is the case where the screen was handed
// four megabytes to hold for the life of the run under one row.
func TestAChangeOfVeryLongLinesIsCappedInBytes(t *testing.T) {
	before := strings.Repeat("a", 2_000_000)
	after := strings.Repeat("b", 2_000_000)

	change := wholeChange(before, after)
	if len(change) > changeMaxBytes+200 {
		t.Errorf("a one-line change of two megabytes each way gave the screen %d bytes, want it capped near %d",
			len(change), changeMaxBytes)
	}
	if !strings.Contains(change, "more bytes of the change") {
		t.Errorf("the cut does not say what is missing:\n…%s", change[len(change)-120:])
	}
	if !strings.Contains(change, "the file on disk has all of it") {
		t.Error("the notice does not say where the rest is")
	}
}

// A change that fits is handed over whole, with no notice on it: the cap is for the case nobody can
// read, not for the ordinary one a reviewer checks line by line.
func TestAnOrdinaryChangeIsNotCutAtAll(t *testing.T) {
	var before, after strings.Builder
	for i := 0; i < 400; i++ {
		before.WriteString("line " + strings.Repeat("x", 40) + " before\n")
		after.WriteString("line " + strings.Repeat("x", 40) + " after\n")
	}
	change := wholeChange(before.String(), after.String())
	if strings.Contains(change, "more bytes") || strings.Contains(change, "more lines") {
		t.Errorf("an ordinary change was cut:\n%s", change[len(change)-200:])
	}
	if !strings.Contains(change, "\n-") || !strings.Contains(change, "\n+") {
		t.Errorf("the change does not read as one: %q", change[:60])
	}
}

// The cut lands on a line break where one is nearby: half a line of minified code reads as a mistake
// rather than as a cut.
func TestAByteCutLandsOnALineBreak(t *testing.T) {
	var b strings.Builder
	for b.Len() < 600_000 {
		b.WriteString(strings.Repeat("z", 300) + "\n")
	}
	long := b.String()
	change := wholeChange(long, strings.ReplaceAll(long, "z", "y"))
	lines := strings.Split(change, "\n")
	body := lines[:len(lines)-1] // the last is the notice
	if last := body[len(body)-1]; strings.HasSuffix(last, "…") || len(last) > 400 {
		t.Errorf("the change stops mid-line: %d columns", len(last))
	}
}
