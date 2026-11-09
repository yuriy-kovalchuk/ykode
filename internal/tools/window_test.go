package tools

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const windowGoal = 4096

// under the budget the window is the whole output, and nothing is written anywhere
func TestAWindowThatFitsHoldsAllOfIt(t *testing.T) {
	logs := t.TempDir()
	w := newOutWindow(windowGoal, logs)
	in := []byte(strings.Repeat("line\n", 100))
	if n, err := w.Write(in); n != len(in) || err != nil {
		t.Fatalf("write took %d of %d bytes: %v", n, len(in), err)
	}
	w.close()
	if got := string(w.head) + string(w.tail); got != string(in) {
		t.Errorf("the window lost output: %q", got)
	}
	if w.total != len(in) {
		t.Errorf("the window counted %d bytes, the command wrote %d", w.total, len(in))
	}
	if w.path != "" {
		t.Errorf("an output that fits was saved to %s", w.path)
	}
	if files, _ := os.ReadDir(logs); len(files) != 0 {
		t.Errorf("an output that fits left %d files behind", len(files))
	}
}

// past the budget the run holds a window, and the file holds everything
func TestAWindowThatOverflowsKeepsItsHeadAndTail(t *testing.T) {
	logs := t.TempDir()
	w := newOutWindow(windowGoal, logs)
	var all bytes.Buffer
	chunk := []byte(strings.Repeat("abcdefgh\n", 40)) // 360 bytes at a time
	for i := 0; all.Len() < 5_000_000; i++ {
		if _, err := w.Write(chunk); err != nil {
			t.Fatal(err)
		}
		all.Write(chunk)
		if i%100 == 0 {
			w.Write([]byte{byte('a' + i%26)}) // a stray byte between writes
			all.Write([]byte{byte('a' + i%26)})
		}
	}
	w.close()
	if w.total != all.Len() {
		t.Errorf("the window counted %d bytes, the command wrote %d", w.total, all.Len())
	}
	if held := len(w.head) + len(w.tail); held > windowGoal {
		t.Errorf("the window holds %d bytes of a %d byte budget", held, windowGoal)
	}
	headWant := windowGoal / budgetSlackDiv
	if string(w.head) != all.String()[:headWant] {
		t.Errorf("the head is not the first %d bytes the command wrote", headWant)
	}
	if string(w.tail) != all.String()[all.Len()-len(w.tail):] {
		t.Errorf("the tail is not the last %d bytes the command wrote", len(w.tail))
	}
	if w.path == "" {
		t.Fatal("an output past its budget left no log behind")
	}
	saved, err := os.ReadFile(w.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, all.Bytes()) {
		t.Errorf("the log holds %d of the %d bytes the command wrote", len(saved), all.Len())
	}
}

// the log keeps going while the command runs, so it has to be closed when the run is done with it
func TestTheWindowClosesItsLog(t *testing.T) {
	w := newOutWindow(64, t.TempDir())
	w.Write([]byte(strings.Repeat("x", 500)))
	w.close()
	w.close() // closing twice says nothing twice
	if w.file != nil {
		t.Error("close left the log open")
	}
	if w.why != nil {
		t.Errorf("closing twice complained: %v", w.why)
	}
	if err := os.RemoveAll(filepath.Dir(w.path)); err != nil {
		t.Errorf("the log could not be removed after close: %v", err)
	}
}

// the budget is counted in bytes, and a rune does not have to end where the budget does
func TestTheWindowClosesAtARune(t *testing.T) {
	w := newOutWindow(12, t.TempDir())
	w.Write([]byte("héllo wörld ünicode"))
	if got := headText(w.head); !utf8Valid(got) {
		t.Errorf("the head ends inside a character: %q", got)
	}
	if got := tailText(w.tail); !utf8Valid(got) {
		t.Errorf("the tail starts inside a character: %q", got)
	}
}

func utf8Valid(s string) bool {
	for _, r := range s {
		if r == 0xFFFD {
			return false
		}
	}
	return true
}

// a log that cannot be written is said so; it never fails the command
func TestASpillThatCannotBeWrittenSaysSo(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := newOutWindow(64, notADir)
	in := []byte(strings.Repeat("y", 500))
	if n, err := w.Write(in); n != len(in) || err != nil {
		t.Fatalf("the window failed the command: %d bytes, %v", n, err)
	}
	w.close()
	if w.why == nil {
		t.Error("a log that could not be written went unremarked")
	}
}
