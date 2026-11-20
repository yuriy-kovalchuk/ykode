package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
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
		// measured in columns: the band pads to the screen's width, and marks take more bytes than columns
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
	log.failed("write_file", "", "user refused")
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

// BenchmarkStreamingRedraw draws as one streamed token does: only the growing block may redraw
func BenchmarkStreamingRedraw(b *testing.B) {
	var log transcript
	for i := 0; i < 100; i++ {
		log.user(fmt.Sprintf("task %d, with a question long enough to wrap over a row or two", i), time.Now())
		log.thinking(strings.Repeat("considering how the pieces fit and what to read next. ", 30))
		log.answer(strings.Repeat("the change goes in the status line, drawn per frame. ", 20))
		log.toolStart("exec_command", `{"command":"grep -rn status internal"}`)
		log.toolEnd("exec_command", `{"command":"grep -rn status internal"}`, "41 matches in 9 files")
	}
	log.answer("a few words of the answer so far ")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		log.answer("more words ")
		_ = log.render(120)
	}
}

// paintedBehind is an escape setting a background: 48 as a parameter, not the tail of 248
var paintedBehind = regexp.MustCompile(`\x1b\[(?:[0-9]+;)*48;`)

// promptTop is how the card an ask sits in opens.
const promptTop = "╭─ you"

// the ask sits in a card across the screen, reading apart from an answer over several rows
func TestAPromptIsACardAcrossTheScreen(t *testing.T) {
	block := strings.TrimRight(strings.Join((&transcript{}).draw(&entry{who: whoUser, text: "count the lines\nand then the ones added since the last release", at: time.Now()}, 60), "\n"), "\n")
	lines := strings.Split(stripCodes(block), "\n")
	if len(lines) < 4 {
		t.Fatalf("the card does not hold both rows of the ask between its edges: %q", block)
	}
	for i, line := range lines {
		if lipgloss.Width(line) != 60 {
			t.Errorf("the card stops short at %d cells: %q", lipgloss.Width(line), line)
		}
		want := "│"
		switch i {
		case 0:
			want = promptTop
		case len(lines) - 1:
			want = "╰"
		}
		if !strings.HasPrefix(line, want) {
			t.Errorf("row %d does not start with %q: %q", i, want, line)
		}
	}
	if !strings.Contains(lines[0], clock(time.Now())) {
		t.Errorf("the card's top edge does not say when it was asked: %q", lines[0])
	}
}

// a command's answer and a notice stay inside the screen, not at the terminal's own break
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
		case strings.Contains(row, "strings.TrimSpace"):
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

// a call carries no ground: its state is the mark's colour, and the row is its words' width
func TestACallIsMarkedByItsStateAndNotPaintedBehindIt(t *testing.T) {
	var log transcript
	args := `{"path":"a.go"}`
	log.toolStart("read_file", args)
	running := callRow(&log, 0)
	log.toolEnd("read_file", args, "read a.go: lines 1-40 of 40")
	done := callRow(&log, 0)
	log.failed("read_file", "", "no such file")
	failed := callRow(&log, 1)
	log.draft(2, "write_file", `{"path":"a.go","content":"package main`)
	writing := callRow(&log, 2)

	for what, want := range map[string]struct{ row, mark string }{
		"a call waiting": {running, "▸"}, "a call finished": {done, "✓"},
		"a call that failed": {failed, "✗"}, "a call being written": {writing, "✎"},
	} {
		if !strings.Contains(stripCodes(want.row), want.mark) {
			t.Errorf("%s lacks its mark %q: %q", what, want.mark, stripCodes(want.row))
		}
		if paintedBehind.MatchString(want.row) || strings.Contains(want.row, "\x1b[7m") {
			t.Errorf("%s is painted behind its words: %q", what, want.row)
		}
	}
	if done == failed || codeOf(styleOK) == codeOf(styleBad) {
		t.Error("finished and failed read the same, so the mark says nothing")
	}
}

// a call's ground is a tint, not the terminal's solid colour: a full row of green is noise
func TestACallsGroundIsQuietEnoughToRead(t *testing.T) {
	var log transcript
	args := `{"path":"a.go"}`
	log.toolStart("read_file", args)
	rows := []string{callRow(&log, 0)}
	log.toolEnd("read_file", args, "read a.go: lines 1-40 of 40")
	rows = append(rows, callRow(&log, 0))
	log.failed("read_file", "", "no such file")
	rows = append(rows, callRow(&log, 1))
	for _, row := range rows {
		for _, solid := range []string{"[40m", "[41m", "[42m", "[43m", "[44m", "[45m", "[46m", "[47m", "[100m"} {
			if strings.Contains(row, solid) {
				t.Errorf("the call sits on a solid terminal colour %s: %q", solid, stripCodes(row))
			}
		}
	}
}

// the ask keeps the whole width; everything else reads inside a margin, short of the edge
func TestAMessageSitsInsideTheMargins(t *testing.T) {
	var log transcript
	log.user("count the lines", time.Now())
	log.answer("the change goes in the status line, drawn per frame by the screen that reads it")
	log.thinking("considering where the width is decided")
	log.toolStart("edit_file", `{"path":"src/a.go"}`)
	log.notice("the model asked for the same read again")

	rows := log.rows(80)
	if ask := rows[log.rowStart[0]]; !strings.HasPrefix(stripCodes(ask), promptTop) || lipgloss.Width(ask) != 80 {
		t.Errorf("the ask does not keep the whole width: %q", stripCodes(ask))
	}
	for _, i := range []int{1, 2, 3, 4} {
		row := rows[log.rowStart[i]]
		if !strings.HasPrefix(row, messageLead) {
			t.Errorf("the message does not start inside the margin: %q", stripCodes(row))
		}
		if got := lipgloss.Width(row); got > 80-messageRight {
			t.Errorf("a message row reaches %d of the 80 columns: %q", got, stripCodes(row))
		}
	}
	if answer := stripCodes(rows[log.rowStart[1]]); strings.HasPrefix(answer, promptTop) {
		t.Errorf("an answer is drawn as a prompt card: %q", answer)
	}
}

// a terminal narrower than the margin still fits: the margin shrinks rather than overflowing
func TestAMarginNeverPushesARowPastANarrowScreen(t *testing.T) {
	var log transcript
	log.answer(strings.Repeat("the change goes in the status line ", 8))
	log.thinking(strings.Repeat("considering where the width is decided ", 6))
	log.notice("the model asked for the same read_file again, which costs a row of its own")

	for _, width := range []int{20, 30, 40} {
		for _, row := range log.rows(width) {
			if row != "" && lipgloss.Width(row) > width {
				t.Errorf("at width %d a row is %d: %q", width, lipgloss.Width(row), stripCodes(row))
			}
		}
	}
}

// callRow is one call's own first row, not the change or failure drawn under it
func callRow(log *transcript, at int) string {
	return log.rows(60)[log.rowStart[at]]
}

// squeezed is a row with its runs of spaces made one, for a check about words rather than columns.
func squeezed(row string) string {
	lines := strings.Split(row, "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.Join(lines, "\n")
}

// a card is as wide as asked, label in the top edge; a narrow screen gets text, not a frame
func TestACardFramesItsRowsAtTheWidthAsked(t *testing.T) {
	rows := card("you", "08:31", []string{"one", "two"}, 30)
	if len(rows) != 4 || !strings.HasSuffix(stripCodes(rows[0]), " 08:31 ─╮") {
		t.Fatalf("card rows: %q", rows)
	}
	for _, row := range rows {
		if lipgloss.Width(row) != 30 {
			t.Errorf("a row is %d wide, want 30: %q", lipgloss.Width(row), stripCodes(row))
		}
	}
	if narrow := card("you", "", []string{"x"}, 3); len(narrow) != 1 || narrow[0] != "x" {
		t.Errorf("a 3-column card: %q", narrow)
	}
	if tight := card("you", "a label far too long for this card", []string{"x"}, 12); lipgloss.Width(tight[0]) != 12 {
		t.Errorf("a label that does not fit pushed the edge to %d columns: %q", lipgloss.Width(tight[0]), stripCodes(tight[0]))
	}
}
