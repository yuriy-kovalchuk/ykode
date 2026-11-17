package tui

import "strings"

// window is the session's slice of the screen, moving a cursor through rows already drawn
type window struct {
	rows   []string
	at     int // the first row in view
	height int // how many rows fit above the prompt
}

// setRows takes the session as drawn: a reader at the end stays there, one scrolled up stays put
func (w *window) setRows(rows []string) {
	atEnd := w.atBottom() // where the reader stood before the session changed
	w.rows = rows
	if atEnd || w.at > w.last() {
		w.at = w.last() // the end, or a session that shrank under the reader
	}
}

// atBottom says the reader is at the end of the session, which is where output arriving belongs.
func (w *window) atBottom() bool { return w.at >= w.last() }

// last is the highest starting row that still fills the view.
func (w *window) last() int { return max(0, len(w.rows)-w.height) }

func (w *window) gotoBottom() { w.at = w.last() }

// scroll moves the view, clamped at both ends.
func (w *window) scroll(rows int) {
	w.at = min(max(w.at+rows, 0), w.last())
}

// percent is how far into the session the view sits, for the mark beside the rule.
func (w *window) percent() float64 {
	if w.last() <= 0 {
		return 1
	}
	return float64(w.at) / float64(w.last())
}

// view draws the rows in view and blanks the rest, keeping the prompt at the screen's bottom
func (w *window) view() string {
	seen := make([]string, max(w.height, 0))
	if len(w.rows) > 0 {
		copy(seen, w.rows[w.at:min(w.at+w.height, len(w.rows))])
	}
	return strings.Join(seen, "\n")
}
