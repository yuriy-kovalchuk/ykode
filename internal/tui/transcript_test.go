package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"
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

// a call being written shows on the screen as it streams, and gives way to the tool row once the reply ends
func TestACallBeingWrittenShowsUntilTheReplyEnds(t *testing.T) {
	var log transcript
	for _, piece := range []string{`{"path":"index.html",`, `"content":"<html>\n<body>\n`, `<p>hi</p>`} {
		log.draft(0, "write_file", piece)
	}
	view := stripCodes(log.render(80, false))
	for _, want := range []string{"write_file", "index.html", "3 lines", "<p>hi</p>"} {
		if !strings.Contains(view, want) {
			t.Errorf("the call being written does not show %q:\n%s", want, view)
		}
	}
	if strings.Count(view, "write_file") != 1 {
		t.Errorf("one call became several rows:\n%s", view)
	}
	log.dropDrafts()
	if strings.Contains(log.render(80, false), "index.html") {
		t.Error("the call being written is still shown after the reply ended")
	}
}

// two calls in one reply are two rows, each growing on its own
func TestTwoCallsBeingWrittenAreTwoRows(t *testing.T) {
	var log transcript
	log.draft(0, "read_file", `{"path":"a.go"`)
	log.draft(1, "read_file", `{"path":"b.go"`)
	log.draft(0, "read_file", `}`)
	if n := len(log.entries); n != 2 || log.entries[0].args != `{"path":"a.go"}` {
		t.Errorf("entries = %+v, want two calls with their own arguments", log.entries)
	}
}

// every prompt says when it was sent, and every finished prompt when it ended and how long it took
func TestMessagesCarryTheirTime(t *testing.T) {
	var log transcript
	sent := time.Date(2026, 11, 8, 8, 31, 0, 0, time.Local)
	log.user("fix the test", sent)
	log.answer("fixed")
	log.done(sent.Add(51*time.Second), 51*time.Second)
	view := stripCodes(log.render(60, false))
	first := strings.Split(view, "\n")[0]
	if !strings.HasSuffix(first, "08:31") || len(first) < 50 {
		t.Errorf("the prompt's time is not at the end of its row: %q", first)
	}
	if !strings.Contains(view, "08:31 · 51s") {
		t.Errorf("the finished prompt has no end time and duration:\n%s", view)
	}
}

// a command's output keeps its own lines: /help is a list, not one squashed sentence
func TestCommandOutputKeepsItsLines(t *testing.T) {
	var log transcript
	log.output("commands:\n  /help   show this\n  /usage  what it cost")
	view := stripCodes(log.render(80, false))
	if !strings.Contains(view, "\n    /help   show this\n") || !strings.Contains(view, "/usage  what it cost") {
		t.Errorf("the output lost its lines:\n%s", view)
	}
}

// a wider screen redraws every row: a row's own drawing was made for the width it was drawn at
func TestWideningTheScreenDrawsEveryRowAgain(t *testing.T) {
	var log transcript
	log.user("a prompt short enough to fit on one row", time.Now())
	narrow := strings.Split(log.render(60, false), "\n")[0]
	log.notice("the run shortened an older result") // the next draw only has to reach the newest row
	if wide := strings.Split(log.render(120, false), "\n")[0]; len(wide) <= len(narrow) {
		t.Errorf("the prompt row kept its %d columns when the screen grew to %d", len(narrow), len(wide))
	}
}

// rows move up when the calls being written are dropped, so none may keep another's drawing
func TestDroppingTheCallsBeingWrittenDrawsTheRowsBelowAgain(t *testing.T) {
	var log transcript
	log.draft(0, "write_file", `{"path":"main.go","content":"package main"`)
	log.toolStart("read_file", `{"path":"go.mod"}`)
	log.dropDrafts()
	if out := log.render(80, false); strings.Contains(out, "✎") {
		t.Errorf("a dropped call is still on the screen:\n%s", out)
	}
}

// BenchmarkStreamingRedraw draws the screen as one streamed token does: a session with a long past
// and one block still growing. Only the block being written may need drawing again.
func BenchmarkStreamingRedraw(b *testing.B) {
	var log transcript
	for i := 0; i < 100; i++ {
		log.user(fmt.Sprintf("task %d, with a question long enough to wrap over a row or two", i), time.Now())
		log.thinking(strings.Repeat("considering how the pieces fit and what to read next. ", 30))
		log.answer(strings.Repeat("the change goes in the status line, drawn per frame. ", 20))
		log.toolStart("search_text", `{"pattern":"status","path":"internal"}`)
		log.toolEnd("search_text", `{"pattern":"status","path":"internal"}`, "41 matches in 9 files")
	}
	log.answer("a few words of the answer so far ")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		log.answer("more words ")
		_ = log.render(120, false)
	}
}
