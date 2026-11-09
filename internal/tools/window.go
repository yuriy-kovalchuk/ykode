package tools

import (
	"os"
	"path/filepath"
	"unicode/utf8"
)

// outWindow is what a command's output costs the run: its head, a rolling tail, and how many bytes
// arrived. Once the output outgrows the budget, every byte is copied to a log file as it arrives, so
// a command that prints for two minutes costs a window of RAM rather than the print.
//
// While nothing has been spilled, head and tail together hold exactly what has been read; that is
// what makes the log complete the moment it is opened.
type outWindow struct {
	head     []byte
	tail     []byte
	total    int
	goal     int    // bytes the result may use
	logs     string // where a cut output's full log goes
	path     string // the full log, once the output outgrew the budget
	why      error  // why there is no full log
	file     *os.File
	headRoom int
	tailRoom int
}

func newOutWindow(goal int, logs string) *outWindow {
	head := goal / budgetSlackDiv
	return &outWindow{goal: goal, logs: logs, headRoom: head, tailRoom: goal - head}
}

// Write takes one piece of the command's output. It never fails the command: a log that cannot be
// written is said so in the result, the same as a spill that used to fail after the fact.
func (w *outWindow) Write(p []byte) (int, error) {
	read := len(p)
	if w.file == nil && w.total+read > w.goal {
		w.open() // writes everything read so far
	}
	if w.file != nil {
		w.copy(p)
	}
	if room := w.headRoom - len(w.head); room > 0 {
		take := min(room, len(p))
		w.head = append(w.head, p[:take]...)
		p = p[take:]
	}
	w.tail = append(w.tail, p...)
	if over := len(w.tail) - w.tailRoom; over > 0 {
		w.tail = w.tail[over:] // the newest bytes are the ones the model wants
	}
	w.total += read
	return read, nil
}

// open starts the full log and writes what has been read so far, which is everything: no byte has
// been dropped before the budget is passed.
func (w *outWindow) open() {
	dir, err := os.MkdirTemp(w.logs, spillPrefix)
	if err != nil {
		w.why = err
		return
	}
	path := filepath.Join(dir, "output.log")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		w.why = err
		return
	}
	w.path, w.file = path, file
	w.copy(append(append([]byte(nil), w.head...), w.tail...))
}

func (w *outWindow) copy(p []byte) {
	if w.file == nil {
		return
	}
	if _, err := w.file.Write(p); err != nil && w.why == nil {
		w.why = err
	}
}

// close ends the log. An open file would keep a descriptor for the life of the run.
func (w *outWindow) close() {
	if w.file != nil {
		if err := w.file.Close(); err != nil && w.why == nil {
			w.why = err
		}
		w.file = nil
	}
}

// headText ends the head at a whole rune: the budget is counted in bytes and a rune does not have to
// end where the budget does.
func headText(b []byte) string {
	end, at := 0, 0
	for at < len(b) {
		r, size := utf8.DecodeRune(b[at:])
		if r == utf8.RuneError && size <= 1 {
			break // the window closed in the middle of a character
		}
		end, at = at+size, at+size
	}
	return string(b[:end])
}

// tailText starts the tail at a whole rune, for the same reason.
func tailText(b []byte) string {
	at := 0
	for at < len(b) && at < utf8.UTFMax && !utf8.RuneStart(b[at]) {
		at++
	}
	return string(b[at:])
}
