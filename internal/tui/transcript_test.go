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
