package tui

import (
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
	out := log.render(80)
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

	if log.entries[0].args != "" {
		t.Errorf("a.go's row is still waiting for the answer it got: %q", log.entries[0].args)
	}
	if !strings.Contains(log.entries[0].call.note, "a has 3 funcs") {
		t.Errorf("a.go's reply landed on another row: %q", log.entries[0].call.note)
	}
	if log.entries[1].args == "" {
		t.Errorf("b.go was answered with a.go's result")
	}
	out := stripCodes(log.render(80))
	if !strings.Contains(out, "▸ read_file b.go") {
		t.Errorf("a call still running does not say it is working:\n%s", out)
	}
}

// every prompt says when it was sent, and every finished prompt when it ended and how long it took
func TestMessagesCarryTheirTime(t *testing.T) {
	var log transcript
	sent := time.Date(2026, 11, 8, 8, 31, 0, 0, time.Local)
	log.user("fix the test", sent)
	log.answer("fixed")
	log.done(sent.Add(51*time.Second), 51*time.Second)
	view := stripCodes(log.render(60))
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
	view := stripCodes(log.render(80))
	if !strings.Contains(view, "\n      /help   show this\n") || !strings.Contains(view, "/usage  what it cost") {
		t.Errorf("the output lost its lines:\n%s", view)
	}
}

// the recap a compaction left behind is drawn where it happened, in the model's own lines
func TestARecapIsDrawnWhereItHappened(t *testing.T) {
	var log transcript
	log.summary("Goal: rename the helper\nFiles: rows.go, bottom.go\nNext: run the tests")

	view := stripCodes(log.render(80))
	if !strings.Contains(view, "the older turns, folded into this") {
		t.Errorf("the recap is not marked as one:\n%s", view)
	}
	for _, want := range []string{"Goal: rename the helper", "Next: run the tests"} {
		if !strings.Contains(view, want) {
			t.Errorf("%q missing from the recap:\n%s", want, view)
		}
	}
}

// the screen reads rows, and they are kept between draws: a change redraws its own block and the
// rows around it stay as they were drawn
func TestTheRowsAreKeptBetweenDraws(t *testing.T) {
	var log transcript
	log.user("the first ask", time.Now())
	log.answer("the first answer")
	before := append([]string(nil), log.rows(60)...)

	log.answer(", and it goes on")
	after := log.rows(60)
	if after[0] != before[0] {
		t.Errorf("a row above the change was drawn again:\n%s\nwant\n%s", after[0], before[0])
	}
	if joined := strings.Join(after, "\n"); !strings.Contains(joined, "it goes on") {
		t.Errorf("the change never reached the rows:\n%s", joined)
	}
	if got, want := log.render(60), strings.TrimRight(strings.Join(after, "\n"), "\n"); got != want {
		t.Errorf("reading the session back differs from its rows:\n%s\nwant\n%s", got, want)
	}
}

// a call answered after it was drawn keeps its row, so the rows below it do not move
func TestACallAnsweredLaterKeepsItsRow(t *testing.T) {
	var log transcript
	log.toolStart("read_file", `{"path":"a.go"}`)
	log.notice("something the run did on its own")
	before := log.rows(60)

	log.toolEnd("read_file", `{"path":"a.go"}`, "read a.go (12 lines)")
	after := log.rows(60)
	if len(after) != len(before) {
		t.Fatalf("answering a call changed how many rows the session takes: %d was %d", len(after), len(before))
	}
	if !strings.Contains(after[0], "read a.go") {
		t.Errorf("the answer did not land on the call's row: %q", after[0])
	}
	if !strings.Contains(after[len(after)-1], "something the run did") {
		t.Errorf("the rows below the call moved: %q", after[len(after)-1])
	}
}

// output that has not been drawn yet is what the "new output below" mark is for
func TestTheSessionKnowsWhatItHasNotDrawnYet(t *testing.T) {
	var log transcript
	if log.hasPending() {
		t.Error("an empty session reports output waiting")
	}
	log.notice("output arrived")
	if !log.hasPending() {
		t.Error("output that was never drawn is not reported as waiting")
	}
	log.rows(60)
	if log.hasPending() {
		t.Error("drawn output is still reported as waiting")
	}
}

// a call that fails closes its own row instead of leaving it asking beside a second row that says why
func TestAFailureLandsOnTheRowOfItsCall(t *testing.T) {
	var log transcript
	args := `{"path":"a.txt","content":"x"}`
	log.toolStart("write_file", args)
	log.failed("write_file", args, "mkdir data: file exists")
	if len(log.entries) != 1 || !log.entries[0].failed || log.entries[0].args != "" {
		t.Fatalf("the failure did not end its call's row: %+v", log.entries)
	}
	if out := stripCodes(log.render(80)); strings.Contains(out, "▸") || !strings.Contains(out, "file exists") {
		t.Errorf("a row is still asking, or the reason is missing:\n%s", out)
	}
}
