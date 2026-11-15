package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func readToolFixture(t *testing.T, content string) *ReadFile {
	t.Helper()
	root, dir := treeFixture(t, map[string]string{"f.txt": content})
	return NewReadFile(root, dir)
}

func TestReadPagesWithAnActionableMarker(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	tool := readToolFixture(t, b.String())

	out, err := tool.Run(context.Background(), json.RawMessage(`{"path":"f.txt","offset":2,"limit":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "f.txt: lines 2-3 of 5\nline 2\nline 3\n") {
		t.Fatalf("window wrong:\n%q", out)
	}
	if !strings.Contains(out, "[2 of 5 lines shown (pass a larger limit); continue with offset=4]") {
		t.Fatalf("marker wrong:\n%s", out)
	}
}

func TestReadCutsByBytesBeforeTheLineLimit(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 1000; i++ {
		fmt.Fprintf(&b, "line %04d %s\n", i, strings.Repeat("x", 60))
	}
	tool := readToolFixture(t, b.String())

	out, err := tool.Run(context.Background(), json.RawMessage(`{"path":"f.txt","limit":1000}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > defaultResultBytes {
		t.Fatalf("result is %d bytes, over the %d byte cap", len(out), defaultResultBytes)
	}
	if !strings.Contains(out, fmt.Sprintf("(%d byte limit); continue with offset=", defaultResultBytes-budgetSlack)) {
		t.Fatalf("marker wrong:\n%s", cutUTF8(out, 200))
	}
	if lines := strings.Count(strings.TrimSpace(out), "\n"); lines < 50 || lines >= 1000 {
		t.Fatalf("expected a partial window, got %d lines", lines)
	}
}

func TestReadShowsCRLFFilesAsLFText(t *testing.T) {
	tool := readToolFixture(t, "alpha\r\nbeta\r\n")
	out, err := tool.Run(context.Background(), json.RawMessage(`{"path":"f.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\r") {
		t.Fatalf("result should not carry carriage returns: %q", out)
	}
}

func TestReadRejectsBinaryAndPastEndOffsets(t *testing.T) {
	tool := readToolFixture(t, "a\x00b\n")
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"path":"f.txt"}`)); err == nil ||
		!strings.Contains(err.Error(), "binary") {
		t.Fatalf("binary error = %v", err)
	}
	text := readToolFixture(t, "one\ntwo\n")
	if _, err := text.Run(context.Background(), json.RawMessage(`{"path":"f.txt","offset":9}`)); err == nil ||
		!strings.Contains(err.Error(), "past the end") {
		t.Fatalf("offset error = %v", err)
	}
}

func TestReadLimitIsClampedToTheMax(t *testing.T) {
	cases := []struct{ in, want int }{{99999, maxReadLimit}, {0, defaultReadLimit}, {-5, defaultReadLimit}, {10, 10}}
	for _, c := range cases {
		if got := clampLimit(c.in, defaultReadLimit, maxReadLimit); got != c.want {
			t.Errorf("clampLimit(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestReadRefusesAFileTooBigToPageThrough(t *testing.T) {
	tool := readToolFixture(t, strings.Repeat("y", maxScannedFileBytes+1))
	_, err := tool.Run(context.Background(), json.RawMessage(`{"path":"f.txt"}`))
	if err == nil || !strings.Contains(err.Error(), "1.0 MB") || !strings.Contains(err.Error(), "grep -n") {
		t.Fatalf("error = %v, want the size and a way forward", err)
	}
}

func TestReadStillReadsAFileAtTheSizeCap(t *testing.T) {
	tool := readToolFixture(t, strings.Repeat("x\n", maxScannedFileBytes/2))
	out, err := tool.Run(context.Background(), json.RawMessage(`{"path":"f.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "continue with offset=2001]") { // one full window of a long file
		t.Fatalf("marker wrong:\n%s", cutUTF8(out, 200))
	}
}

func TestReadRefusesADirectoryWithAHint(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"src/main.go": "package main\n"})
	_, err := NewReadFile(root, dir).Run(context.Background(), json.RawMessage(`{"path":"src"}`))
	if err == nil || !strings.Contains(err.Error(), "exec_command (ls)") {
		t.Errorf("err = %v, want it to point at a way to list the directory", err)
	}
}

func TestSettingsSetTheReadingWindow(t *testing.T) {
	body := strings.Repeat("line\n", 500)
	root, dir := treeFixture(t, map[string]string{"many.txt": body})
	tool := NewReadFile(root, dir)
	tool.Lines = 10

	out := runTool(t, tool, `{"path":"many.txt"}`)
	if !strings.HasPrefix(out, "many.txt: lines 1-10 of 500\n") {
		t.Fatalf("window header wrong:\n%q", cutUTF8(out, 120))
	}
	if got := strings.Count(out, "line\n"); got != 10 {
		t.Fatalf("%d lines shown, want the configured window of 10", got)
	}
	if !strings.Contains(out, "continue with offset=11") {
		t.Fatalf("no continuation marker:\n%s", out)
	}
}

func TestPlainTextInsteadOfLineNumbers(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"src/main.go": "package main\n\nfunc main() {}\n"})

	out := runTool(t, NewReadFile(root, dir), `{"path":"src/main.go"}`)
	want := "src/main.go: lines 1-3 of 3\npackage main\n\nfunc main() {}\n"
	if out != want {
		t.Fatalf("read_file = %q, want %q", out, want)
	}
}

// a second read of the same window returns the same text: the tool keeps no memory of the first
func TestReadingTheSameWindowTwiceReturnsTheTextTwice(t *testing.T) {
	tool := readToolFixture(t, numbered(40, "line"))
	args := `{"path":"f.txt","limit":20}`
	first := runTool(t, tool, args)
	second := runTool(t, tool, args)
	if first != second {
		t.Errorf("the second read differs from the first:\n%s", second)
	}
}

func TestASmallFileComesBackWhole(t *testing.T) {
	tool := readToolFixture(t, numbered(300, "line"))
	tool.Lines = defaultReadLimit // what the settings always pass, so the rule must not depend on it being unset
	out := runTool(t, tool, `{"path":"f.txt"}`)
	if !strings.HasPrefix(out, "f.txt: lines 1-300 of 300\n") || strings.Contains(out, "continue with") {
		t.Errorf("a 300-line file took more than one read:\n%s", cutUTF8(out, 120))
	}
	if out := runTool(t, readToolFixture(t, numbered(300, "line")), `{"path":"f.txt","limit":20}`); !strings.HasPrefix(out, "f.txt: lines 1-20 of 300\n") {
		t.Errorf("an explicit limit was not kept:\n%s", cutUTF8(out, 120))
	}
}

// a file that fits the byte budget comes back in one call, as pi's read does; a longer one is
// cut at the budget and says how to get further
func TestABigFileIsReadUpToTheBudget(t *testing.T) {
	tool := readToolFixture(t, numbered(900, "line"))
	tool.budget = 51200 // what a 128k window gives one result
	if out := runTool(t, tool, `{"path":"f.txt"}`); !strings.HasPrefix(out, "f.txt: lines 1-900 of 900\n") || strings.Contains(out, "continue with") {
		t.Errorf("a 900-line file took more than one read:\n%s", cutUTF8(out, 120))
	}
	out := runTool(t, readToolFixture(t, numbered(3000, "line")), `{"path":"f.txt"}`)
	if !strings.Contains(out, "grep -n") || !strings.Contains(out, "continue with offset=") {
		t.Errorf("a cut read does not say how to get further:\n%s", out[max(0, len(out)-300):])
	}
}
