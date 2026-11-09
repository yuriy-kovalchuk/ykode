package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// a log of numbered lines, big enough that the page has to come out of its middle
func spillFixture(t *testing.T, lines int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "output.log")
	var b strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&b, "L%04d a line of command output of a decent length\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func cutResult(path string) string {
	return "exit 1\n\n[... 30000 of 40000 bytes omitted ... full output: " + path +
		" (read it with exec_command, e.g. sed -n '1,80p' FILE)]\n\nthe tail"
}

// an opened call shows what the cut left out, read from the file it was written to
func TestAnOpenedCallShowsAPageOfItsSavedOutput(t *testing.T) {
	path := spillFixture(t, 1000) // ~49 KB, so a quarter in is around line 250
	rows := spillRows(cutResult(path), 100)
	if rows == nil {
		t.Fatal("an opened call showed nothing of the output its row cut away")
	}
	if !strings.Contains(stripCodes(rows[0]), "saved output") || !strings.Contains(stripCodes(rows[0]), "a page from") {
		t.Errorf("the page does not say what it is: %q", stripCodes(rows[0]))
	}
	if wide := spillRows(cutResult(path), 200); !strings.Contains(stripCodes(wide[0]), path) {
		t.Errorf("on a wide screen the page should name the file: %q", stripCodes(wide[0]))
	}
	body := stripCodes(strings.Join(rows, "\n"))
	if !strings.Contains(body, "L02") {
		t.Errorf("the page is not from the middle of the log:\n%s", body)
	}
	if strings.Contains(body, "L0001") {
		t.Errorf("the page repeats the head the row already shows:\n%s", body)
	}
	if len(rows) > spillPageRows+2 {
		t.Errorf("the page takes %d rows, want at most %d", len(rows), spillPageRows+1)
	}
}

// a page of output stops at the edge of the screen like every other row
func TestAPageOfSavedOutputStopsAtTheScreenEdge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.log")
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString(strings.Repeat("wide", 130) + "\n") // 520 columns a line
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, row := range spillRows(cutResult(path), 44) {
		if got := lipgloss.Width(stripCodes(row)); got > 44 {
			t.Errorf("a row takes %d columns of a 44 column screen: %q", got, stripCodes(row))
		}
	}
}

// a log that aged out of the log dir is not the screen's problem
func TestALogThatAgedOutSaysNothing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone", "output.log")
	if rows := spillRows(cutResult(missing), 100); rows != nil {
		t.Errorf("a missing log produced %d rows", len(rows))
	}
}

// a call whose output fit is not a page
func TestACallThatWasNotCutShowsNoPage(t *testing.T) {
	if rows := spillRows("exit 0, no output: the command succeeded", 100); rows != nil {
		t.Errorf("an output that was not cut produced %d rows", len(rows))
	}
}

// the page belongs to the opened call, not to the session as it streams past
func TestThePageIsDrawnOnlyWhileTheCallIsOpened(t *testing.T) {
	path := spillFixture(t, 1000)
	e := &entry{who: whoTool, tool: "make", args: `{"command":"make"}`, result: cutResult(path)}
	if rows := draw(e, 100, false); strings.Contains(strings.Join(rows, "\n"), "L02") {
		t.Errorf("a closed call showed its saved log:\n%s", strings.Join(rows, "\n"))
	}
	rows := strings.Join(draw(e, 100, true), "\n")
	if !strings.Contains(rows, "L02") {
		t.Errorf("an opened call showed no page of the output it cut:\n%s", rows)
	}
}
