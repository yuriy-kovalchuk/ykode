package tui

import (
	"io"
	"os"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/size"
)

const (
	spillPageRows  = 20      // lines of a saved command log an opened call shows
	spillLookBytes = 8 << 10 // reading a page never costs more than this
	spillMark      = "  ┊ "
)

// spillPath is where a cut command's whole output went, from the note the tool writes.
func spillPath(result string) string {
	_, rest, found := strings.Cut(result, "full output: ")
	if !found {
		return ""
	}
	path, _, _ := strings.Cut(rest, " (")
	return path
}

// spillRows is a page out of the middle of that log. The command's head and tail are already on the
// screen, so what a reader cannot see is what the cut left out; the file holds it, so the page is
// read from there rather than kept in RAM for the life of the run.
func spillRows(result string, width int) []string {
	path := spillPath(result)
	if path == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil // the log aged out: the call's own row already says where it was
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() <= spillLookBytes {
		return nil // a log this small was never cut in the first place
	}
	at := info.Size() / 4
	if _, err := file.Seek(at, io.SeekStart); err != nil {
		return nil
	}
	page := make([]byte, spillLookBytes)
	read, err := io.ReadAtLeast(file, page, len(page))
	if err != nil && read == 0 {
		return nil
	}
	head := "saved output " + size.Text(int(info.Size())) + " · a page from " + size.Text(int(at)) + " in · " + path
	rows := []string{styleDim.Render(spillMark + oneLine(head, width-len(spillMark)))}
	lines := strings.Split(string(page[:read]), "\n")
	lines = lines[1:] // the first line is cut in half: the page starts mid-line
	for _, line := range lines {
		if len(rows) > spillPageRows {
			rows = append(rows, styleDim.Render(spillMark+oneLine("… "+size.Text(int(info.Size()-at))+" further to the end", width-len(spillMark))))
			break
		}
		if line == "" {
			continue
		}
		rows = append(rows, spillMark+oneLine(line, width-len(spillMark)))
	}
	return rows
}
