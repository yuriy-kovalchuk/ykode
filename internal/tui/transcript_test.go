package tui

import (
	"strings"
	"testing"
)

func TestThinkingArrivesInPiecesAndReadsAsOneBlock(t *testing.T) {
	var log transcript
	log.thinking("the bug ")
	log.thinking("is in main.go")
	log.answer("fix it here")

	if len(log.entries) != 2 {
		t.Fatalf("want one thinking block and one answer, got %d", len(log.entries))
	}
	if log.entries[0].text != "the bug is in main.go" {
		t.Errorf("the pieces did not join: %q", log.entries[0].text)
	}
	out := log.render(80, false)
	if !strings.Contains(out, "the bug is in main.go") || !strings.Contains(out, "fix it here") {
		t.Errorf("rendered transcript is missing what was said:\n%s", out)
	}
}

func TestAParallelReplyLandsOnItsOwnCall(t *testing.T) {
	var log transcript
	log.toolStart("read_file", `{"path":"a.go"}`)
	log.toolStart("read_file", `{"path":"b.go"}`)
	// the first call answers last: the reply has to find its own row, not the newest one
	log.toolEnd("read_file", `{"path":"a.go"}`, "a has 3 funcs")

	if log.entries[0].result != "a has 3 funcs" {
		t.Errorf("a.go's reply landed on another row: %q", log.entries[0].result)
	}
	if log.entries[1].result != "" {
		t.Errorf("b.go was answered with a.go's result: %q", log.entries[1].result)
	}
	out := log.render(80, false)
	if !strings.Contains(out, "working…") {
		t.Errorf("a call still running should say so:\n%s", out)
	}
}

func TestToolRowsShowMoreWhenOpened(t *testing.T) {
	var log transcript
	log.toolStart("edit_file", `{"path":"src/a.go","old_string":"one very long argument list that does not fit a line"}`)
	log.toolEnd("edit_file", `{"path":"src/a.go","old_string":"one very long argument list that does not fit a line"}`, "1 edit applied")

	long := `{"path":"src/a.go","old_string":"one very long argument list that does not fit a line"}`
	collapsed := log.render(80, false)
	if strings.Contains(collapsed, "does not fit a line") {
		t.Errorf("a closed row ran past the result:\n%s", collapsed)
	}
	opened := log.render(80, true)
	if !strings.Contains(opened, `a line"}`) {
		t.Errorf("opening the row did not show what it was asked to do:\n%s", opened)
	}
	for _, line := range strings.Split(collapsed, "\n") {
		if len(stripCodes(line)) > 80 {
			t.Errorf("a row is wider than the screen: %q", stripCodes(line))
		}
	}
	if log.render(40, false) == "" {
		t.Error("a narrow screen rendered nothing")
	}
	if !strings.Contains(log.render(40, false), long[:12]) {
		t.Error("even a narrow row lost the path")
	}
}

func TestAFailedCallIsMarkedAsOne(t *testing.T) {
	var log transcript
	log.failed("write_file", "user refused")
	out := log.render(80, false)
	if !strings.Contains(out, "✗") || !strings.Contains(out, "user refused") {
		t.Errorf("a refused call is not obvious:\n%s", out)
	}
}

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
