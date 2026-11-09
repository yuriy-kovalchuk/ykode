package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// a call row says what the call touches and never holds the text it was asked to write
func TestACallRowSaysWhatItTouches(t *testing.T) {
	args := `{"path":"src/a.go","old_string":"one very long argument list that does not fit a line","new_string":"a shorter one"}`
	var log transcript
	log.toolStart("edit_file", args)
	log.toolEnd("edit_file", args, "1 edit applied")

	out := log.render(80)
	if !strings.Contains(out, "src/a.go") {
		t.Errorf("the row does not say what the call touches:\n%s", out)
	}
	if strings.Contains(out, "does not fit a line") {
		t.Errorf("the row ran past the result into the arguments:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		// measured in columns: the band pads a row out to the width of the screen, and the marks in a
		// row take more bytes than they do columns
		if got := lipgloss.Width(stripCodes(line)); got > 80 {
			t.Errorf("a call row is wider than the screen: %d columns %q", got, stripCodes(line))
		}
	}
	if narrow := log.render(40); narrow == "" {
		t.Error("a narrow screen rendered nothing")
	} else if !strings.Contains(narrow, "src/a.go") {
		t.Error("even a narrow row lost the path")
	}
}

func TestAFailedCallIsMarkedAsOne(t *testing.T) {
	var log transcript
	log.failed("write_file", "user refused")
	out := log.render(80)
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
	view := stripCodes(log.render(80))
	for _, want := range []string{"write_file", "index.html", "3 lines", "<p>hi</p>"} {
		if !strings.Contains(view, want) {
			t.Errorf("the call being written does not show %q:\n%s", want, view)
		}
	}
	if strings.Count(view, "write_file") != 1 {
		t.Errorf("one call became several rows:\n%s", view)
	}
	log.dropDrafts()
	if strings.Contains(log.render(80), "index.html") {
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
	narrow := strings.Split(log.render(60), "\n")[0]
	log.notice("the run shortened an older result") // the next draw only has to reach the newest row
	if wide := strings.Split(log.render(120), "\n")[0]; len(wide) <= len(narrow) {
		t.Errorf("the prompt row kept its %d columns when the screen grew to %d", len(narrow), len(wide))
	}
}

// rows move up when the calls being written are dropped, so none may keep another's drawing
func TestDroppingTheCallsBeingWrittenDrawsTheRowsBelowAgain(t *testing.T) {
	var log transcript
	log.draft(0, "write_file", `{"path":"main.go","content":"package main"`)
	log.toolStart("read_file", `{"path":"go.mod"}`)
	log.dropDrafts()
	if out := log.render(80); strings.Contains(out, "✎") {
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
		_ = log.render(120)
	}
}

// what was asked sits in a band across the whole screen, so it reads apart from an answer even
// when the ask runs over several rows
func TestAPromptIsABandAcrossTheScreen(t *testing.T) {
	block := strings.TrimRight(strings.Join((&transcript{}).draw(&entry{who: whoUser, text: "count the lines\nand then the ones added since the last release", at: time.Now()}, 60), "\n"), "\n")
	lines := strings.Split(block, "\n")
	if len(lines) < 2 {
		t.Fatalf("the band does not reach over more than one row: %q", block)
	}
	// the test terminal has no colours, so what is checked here is the row the band style is laid
	// over: full width, and marked at the left on every row.
	for _, line := range lines {
		if lipgloss.Width(line) != 60 {
			t.Errorf("the band stops short at %d cells: %q", lipgloss.Width(line), line)
		}
		if !strings.HasPrefix(stripCodes(line), userMark) {
			t.Errorf("the row does not start at the mark: %q", stripCodes(line))
		}
	}
}

// a command's answer and a notice stay inside the screen rather than running to the terminal's
// own break, which is where their columns go missing
func TestACommandAnswerStaysInsideTheScreen(t *testing.T) {
	long := "  /history [what] what this run did: " + strings.Repeat("a note long enough to run past the screen ", 3)
	var log transcript
	log.output(long)
	log.notice("the model kept asking for the same call: " + strings.Repeat("stopping the tools and asking for an answer in text ", 3))

	view := log.render(80)
	for _, row := range strings.Split(view, "\n") {
		if row != "" && lipgloss.Width(row) > 80 {
			t.Errorf("a row of %d columns on an 80 column screen: %q", lipgloss.Width(row), row)
		}
	}
}

// a command list keeps its columns: only a line too long for the screen is re-flowed
func TestACommandListKeepsItsColumns(t *testing.T) {
	long := "commands:\n  /help   show this\n  /history [what] " + strings.Repeat("a note long enough to run past the screen ", 3)
	var log transcript
	log.output(long)

	view := stripCodes(log.render(80))
	if !strings.Contains(view, "  /help   show this") {
		t.Errorf("the short row was re-flowed:\n%s", view)
	}
	for _, row := range strings.Split(view, "\n") {
		if row != "" && lipgloss.Width(row) > 80 {
			t.Errorf("a row of %d columns on an 80 column screen: %q", lipgloss.Width(row), row)
		}
	}
}

// a call row written in wide characters still stops at the edge of the screen
func TestAWideToolRowStopsAtTheScreenEdge(t *testing.T) {
	args := `{"path":"src/コード/モジュール.ts","content":"テキスト ".repeat(40)}`
	var log transcript
	log.toolStart("write_file", args)
	log.toolEnd("write_file", args, "書き込み 12 行 · ok")

	for _, line := range strings.Split(log.render(44), "\n") {
		if got := lipgloss.Width(stripCodes(line)); got > 44 {
			t.Errorf("a row takes %d columns: %q", got, stripCodes(line))
		}
	}
}

// a call that changed a file keeps its summary on its row and shows the change on the rows under it
func TestACallRowKeepsItsSummaryApartFromItsChange(t *testing.T) {
	var log transcript
	log.toolStart("edit_file", `{"path":"src/parser.go"}`)
	log.toolEnd("edit_file", `{"path":"src/parser.go"}`, editResult)
	rows := strings.Split(strings.TrimSuffix(log.render(80), "\n"), "\n")

	var summary, change string
	for _, row := range rows {
		switch {
		case strings.Contains(row, "edit_file"):
			summary = row
		case strings.Contains(row, "@@"):
			change = row
		}
	}
	if summary == "" || change == "" {
		t.Fatalf("the call row and its change are not both drawn:\n%s", strings.Join(rows, "\n"))
	}
	if strings.Contains(summary, "@@") || strings.Contains(summary, "if line ==") {
		t.Errorf("the call row carries the change as one flat line: %q", stripCodes(summary))
	}
}

// a call is read as an execution of its own: it sits in a band of colour that says what it did, the
// way pi paints a tool execution.
func TestACallSitsInABandThatSaysWhatItDid(t *testing.T) {
	withColour(t)
	var log transcript
	args := `{"path":"a.go"}`
	log.toolStart("read_file", args)
	running := callRow(&log, 0)
	log.toolEnd("read_file", args, "read a.go: lines 1-40 of 40")
	done := callRow(&log, 0)
	log.failed("read_file", "no such file")
	failed := callRow(&log, 1)
	log.draft(2, "write_file", `{"path":"a.go","content":"package main`)
	writing := callRow(&log, 2)

	checks := []struct {
		what  string
		row   string
		style lipgloss.Style
	}{
		{"a call waiting", running, surfaces().wait},
		{"a call finished", done, surfaces().done},
		{"a call that failed", failed, surfaces().fail},
		{"a call being written", writing, surfaces().wait},
	}
	for _, c := range checks {
		if !strings.Contains(c.row, codeOf(c.style)) {
			t.Errorf("%s is not drawn in a band of its own: %q", c.what, stripCodes(c.row))
		}
	}
	if codeOf(surfaces().wait) == codeOf(surfaces().done) || codeOf(surfaces().done) == codeOf(surfaces().fail) {
		t.Error("waiting, finished and failed share a colour, so the band says nothing")
	}
	for _, row := range []string{running, done, failed, writing} {
		if got := lipgloss.Width(stripCodes(row)); got != 60 {
			t.Errorf("the band takes %d columns of a 60 column screen: %q", got, stripCodes(row))
		}
	}
}

// firstRow is the first row of a block, which is what carries the call.
func firstRow(block []string) string { return block[0] }

// callRow is one call's row: its own first row, which is where the call sits, and not the change or
// the failure drawn under it.
func callRow(log *transcript, at int) string {
	return log.rows(60)[log.rowStart[at]]
}

// the ground a call sits in is a tint, not the terminal's solid colour: a whole row of green or red
// is noise and nothing on it can be read.
func TestACallsGroundIsQuietEnoughToRead(t *testing.T) {
	withColour(t)
	var log transcript
	args := `{"path":"a.go"}`
	log.toolStart("read_file", args)
	rows := []string{callRow(&log, 0)}
	log.toolEnd("read_file", args, "read a.go: lines 1-40 of 40")
	rows = append(rows, callRow(&log, 0))
	log.failed("read_file", "no such file")
	rows = append(rows, callRow(&log, 1))
	for _, row := range rows {
		for _, solid := range []string{"[40m", "[41m", "[42m", "[43m", "[44m", "[45m", "[46m", "[47m", "[100m"} {
			if strings.Contains(row, solid) {
				t.Errorf("the call sits on a solid terminal colour %s: %q", solid, stripCodes(row))
			}
		}
	}
}
