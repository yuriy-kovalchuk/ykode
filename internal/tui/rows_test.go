package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestToolRowsShowMoreWhenOpened(t *testing.T) {
	args := `{"path":"src/a.go","old_string":"one very long argument list that does not fit a line","new_string":"a shorter one"}`
	var log transcript
	log.toolStart("edit_file", args)
	log.toolEnd("edit_file", args, "1 edit applied")

	collapsed := log.render(80, false)
	if strings.Contains(collapsed, "does not fit a line") {
		t.Errorf("a closed row ran past the result:\n%s", collapsed)
	}
	if !strings.Contains(collapsed, "src/a.go") {
		t.Errorf("a closed row does not say what the call touches:\n%s", collapsed)
	}

	opened := log.render(80, true)
	if !strings.Contains(opened, "old_string:") || !strings.Contains(opened, "does not fit a line") {
		t.Errorf("opening the row did not show what it was asked to do:\n%s", opened)
	}
	for _, line := range strings.Split(opened, "\n") {
		// measured in columns: the band pads a row out to the width of the screen, and the marks in a
		// row take more bytes than they do columns
		if got := lipgloss.Width(stripCodes(line)); got > 80 {
			t.Errorf("an opened row is wider than the screen: %d columns %q", got, stripCodes(line))
		}
	}
	if log.render(40, false) == "" {
		t.Error("a narrow screen rendered nothing")
	}
	if !strings.Contains(log.render(40, false), "src/a.go") {
		t.Error("even a narrow row lost the path")
	}
}

// an opened call shows the text an argument holds, and says how much of it it left out
func TestAnOpenedCallShowsTheTextItsArgumentHolds(t *testing.T) {
	content := strings.Repeat("a line of the file\n", 40)
	row := stripCodes(openArgs(`{"path":"big.go","content":`+jsonQuote(content)+`}`, 60))
	if !strings.Contains(row, "path:") || !strings.Contains(row, "a line of the file") {
		t.Errorf("the fields are not on rows of their own:\n%s", row)
	}
	if !strings.Contains(row, "rows more") {
		t.Errorf("long text was not reported as cut:\n%s", row)
	}
	if strings.Contains(row, `\n`) {
		t.Errorf("the text is still escaped JSON:\n%s", row)
	}
}

// jsonQuote puts text in JSON quotes the way the model does
func jsonQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
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

// what was asked sits in a band across the whole screen, so it reads apart from an answer even
// when the ask runs over several rows
func TestAPromptIsABandAcrossTheScreen(t *testing.T) {
	block := strings.TrimRight(draw(&entry{who: whoUser, text: "count the lines\nand then the ones added since the last release", at: time.Now()}, 60, false), "\n")
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

	view := log.render(80, false)
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

	view := stripCodes(log.render(80, false))
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

	for _, line := range strings.Split(log.render(44, false), "\n") {
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
	rows := strings.Split(strings.TrimSuffix(log.render(80, false), "\n"), "\n")

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
	running := firstRow(draw(&entry{who: whoTool, tool: "read_file", args: `{"path":"a.go"}`}, 60, false))
	done := firstRow(draw(&entry{who: whoTool, tool: "read_file", args: `{"path":"a.go"}`, result: "read 40 lines"}, 60, false))
	failed := firstRow(draw(&entry{who: whoTool, tool: "read_file", args: `{"path":"a.go"}`, result: "no such file", failed: true}, 60, false))
	writing := firstRow(draw(newDraft(1, "write_file", `{"path":"a.go","content":"package main`), 60, false))

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
func firstRow(block string) string { return strings.SplitN(block, "\n", 2)[0] }

// the ground a call sits in is a tint, not the terminal's solid colour: a whole row of green or red
// is noise and nothing on it can be read.
func TestACallsGroundIsQuietEnoughToRead(t *testing.T) {
	withColour(t)
	rows := []string{
		firstRow(draw(&entry{who: whoTool, tool: "read_file", args: `{"path":"a.go"}`}, 60, false)),
		firstRow(draw(&entry{who: whoTool, tool: "read_file", args: `{"path":"a.go"}`, result: "read 40 lines"}, 60, false)),
		firstRow(draw(&entry{who: whoTool, tool: "read_file", args: `{"path":"a.go"}`, result: "no such file", failed: true}, 60, false)),
	}
	for _, row := range rows {
		for _, solid := range []string{"[40m", "[41m", "[42m", "[43m", "[44m", "[45m", "[46m", "[47m", "[100m"} {
			if strings.Contains(row, solid) {
				t.Errorf("the call sits on a solid terminal colour %s: %q", solid, stripCodes(row))
			}
		}
	}
}
