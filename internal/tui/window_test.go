package tui

import (
	"strings"
	"testing"
)

func sessionRows(from, to int) []string {
	rows := make([]string, 0, to-from)
	for i := from; i < to; i++ {
		rows = append(rows, "row "+string(rune('0'+i%10)))
	}
	return rows
}

// the window shows the rows it was handed, starting where the reader stands
func TestTheWindowShowsTheRowsItWasHanded(t *testing.T) {
	w := window{height: 4}
	w.setRows(sessionRows(1, 21))
	w.scroll(-100) // the test wants a known place, not wherever the end was
	w.scroll(6)

	got := strings.Split(w.view(), "\n")
	if len(got) != 4 {
		t.Fatalf("the window drew %d rows, want its 4 rows:\n%q", len(got), got)
	}
	if got[0] != "row 7" {
		t.Errorf("the window starts at %q, want the row the reader scrolled to", got[0])
	}
}

// scrolling stops at both ends of the session rather than running off it
func TestScrollingStopsAtTheEndsOfTheSession(t *testing.T) {
	w := window{height: 5}
	w.setRows(sessionRows(1, 11))

	w.scroll(-100)
	if w.at != 0 || !strings.HasPrefix(w.view(), "row 1") {
		t.Errorf("scrolling past the top moved it to %d", w.at)
	}
	w.scroll(100)
	if w.at != w.last() || w.at != 5 {
		t.Errorf("scrolling past the bottom moved it to %d, want %d", w.at, 5)
	}
	if !w.atBottom() {
		t.Error("the reader at the last row does not read as being at the end")
	}
}

// a reader at the end stays there as the session grows, and one who scrolled up is not moved
func TestTheReaderStaysWhereTheyWereAsTheSessionGrows(t *testing.T) {
	w := window{height: 5}
	w.setRows(sessionRows(1, 11))
	w.gotoBottom()
	w.setRows(sessionRows(1, 16))
	if !w.atBottom() {
		t.Errorf("output arriving lifted the reader off the end: at %d of %d", w.at, w.last())
	}

	scrolled := window{height: 5}
	scrolled.setRows(sessionRows(1, 21))
	scrolled.scroll(-4)
	seen := scrolled.view()
	scrolled.setRows(sessionRows(1, 41))
	if scrolled.view() != seen {
		t.Errorf("the session grew under a reader who was reading it:\n%s\nwant\n%s", scrolled.view(), seen)
	}
}

// the session always fills its part of the screen, so the prompt stays at the bottom of it
func TestAShortSessionStillFillsItsPartOfTheScreen(t *testing.T) {
	w := window{height: 6}
	w.setRows(sessionRows(1, 3))

	if got := strings.Split(w.view(), "\n"); len(got) != 6 {
		t.Errorf("a short session drew %d rows, want the 6 the screen gave", len(got))
	}
	w.setRows(nil)
	if got := strings.Split(w.view(), "\n"); len(got) != 6 {
		t.Errorf("an empty session drew %d rows, want the 6 the screen gave", len(got))
	}
}

// a session that got shorter while the reader was near its end does not leave them past the end
func TestASessionThatGotShorterKeepsTheReaderOnIt(t *testing.T) {
	w := window{height: 5}
	w.setRows(sessionRows(1, 41))
	w.gotoBottom()
	w.setRows(sessionRows(1, 6))

	if w.at != w.last() || !w.atBottom() {
		t.Errorf("the reader stayed at %d of a session that ends at %d", w.at, w.last())
	}
}

// the mark beside the rule says how far into the session the reader stands
func TestTheMarkKnowsHowFarIntoTheSession(t *testing.T) {
	w := window{height: 5}
	w.setRows(sessionRows(1, 11))
	w.scroll(-100)
	if got := w.percent(); got != 0 {
		t.Errorf("the top of a session that needs scrolling reports %.0f%%, want 0%%", got*100)
	}
	w.scroll(2)
	if got := w.percent(); got != 0.4 {
		t.Errorf("two rows into a ten row scroll reports %.0f%%, want 40%%", got*100)
	}
}
