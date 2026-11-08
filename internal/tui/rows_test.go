package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

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
