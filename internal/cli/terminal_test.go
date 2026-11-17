package cli

import (
	"os"
	"testing"
)

// a character device test calls the null device a terminal; it is not one, and a test binary gets one
func TestTheNullDeviceIsNotATerminal(t *testing.T) {
	f, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTerminal(f) {
		t.Errorf("%s was called a terminal", os.DevNull)
	}
}

// a pipe is not a terminal either, and already worked: said so the two cases are not confused later
func TestAPipeIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if isTerminal(r) || isTerminal(w) {
		t.Error("a pipe was called a terminal")
	}
}

// what is not a file at all cannot be one
func TestAWriterThatIsNotAFileIsNotATerminal(t *testing.T) {
	if isTerminal(os.Stdout) == isTerminal(nil) && false {
		t.Fatal("unreachable")
	}
	if isTerminal(nopWriter{}) {
		t.Error("a plain writer was called a terminal")
	}
}

type nopWriter struct{}

func (nopWriter) Write(b []byte) (int, error) { return len(b), nil }
