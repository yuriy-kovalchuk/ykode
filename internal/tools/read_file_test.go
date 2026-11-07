package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	if err == nil || !strings.Contains(err.Error(), "1.0 MB") || !strings.Contains(err.Error(), "search_text") {
		t.Fatalf("error = %v, want the size and a way forward", err)
	}
}

func TestReadStillReadsAFileAtTheSizeCap(t *testing.T) {
	tool := readToolFixture(t, strings.Repeat("x\n", maxScannedFileBytes/2))
	out, err := tool.Run(context.Background(), json.RawMessage(`{"path":"f.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "continue with offset=151]") {
		t.Fatalf("marker wrong:\n%s", cutUTF8(out, 200))
	}
}

func TestReadRefusesADirectoryWithAHint(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"src/main.go": "package main\n"})
	_, err := NewReadFile(root, dir).Run(context.Background(), json.RawMessage(`{"path":"src"}`))
	if err == nil || !strings.Contains(err.Error(), "find_files") {
		t.Errorf("err = %v, want it to point at find_files with a depth", err)
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

func TestOutlineListsGoDeclarationsWithRanges(t *testing.T) {
	body := `package main

import "fmt"

func adder(a, b int) int {
	return a + b
}

type config struct {
	name string
}

func main() { fmt.Println(adder(1, 2)) }
`
	root, dir := treeFixture(t, map[string]string{"src/main.go": body})

	out := runTool(t, NewReadFile(root, dir), `{"path":"src/main.go","outline":true}`)
	for _, want := range []string{"lines 5-7: func adder(a, b int) int {", "lines 9-11: type config struct {", "lines 13-13: func main() { fmt.Println(adder(1, 2)) }"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from:\n%s", want, out)
		}
	}
	if strings.Contains(out, "return a + b") {
		t.Error("the outline must not carry the body")
	}
}

func TestOutlineFallsBackToKeywords(t *testing.T) {
	body := "class Thing:\n    def one(self):\n        pass\n\n    def two(self):\n        pass\n"
	root, dir := treeFixture(t, map[string]string{"thing.py": body})

	out := runTool(t, NewReadFile(root, dir), `{"path":"thing.py","outline":true}`)
	for _, want := range []string{"line 1: class Thing:", "line 2: def one(self):", "line 5: def two(self):"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from:\n%s", want, out)
		}
	}
}

func TestALongOutlineIsCutWithAHint(t *testing.T) {
	var b strings.Builder
	b.WriteString("package main\n\n")
	for i := 1; i <= 120; i++ {
		fmt.Fprintf(&b, "func f%03d() {}\n", i)
	}
	root, dir := treeFixture(t, map[string]string{"many.go": b.String()})

	out := runTool(t, NewReadFile(root, dir), `{"path":"many.go","outline":true}`)
	if !strings.Contains(out, "list cut") {
		t.Fatalf("no cut marker in a %d byte outline", len(out))
	}
	if len(out) > 12000 {
		t.Fatalf("outline is %d bytes, over the tool budget", len(out))
	}
}

func TestARepeatOfTheSameWindowCostsOneLine(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"src/main.go": strings.Repeat("package main\n", 30)})
	tool := NewReadFile(root, dir)

	first := runTool(t, tool, `{"path":"src/main.go","offset":1,"limit":10}`)
	if !strings.Contains(first, "lines 1-10 of 30") {
		t.Fatalf("first read looks wrong:\n%s", first)
	}
	again := runTool(t, tool, `{"path":"src/main.go","offset":1,"limit":10}`)
	if !strings.Contains(again, "unchanged since you read lines 1-10") {
		t.Fatalf("the repeat came back in full (%d bytes):\n%s", len(again), again)
	}
	if next := runTool(t, tool, `{"path":"src/main.go","offset":11,"limit":10}`); !strings.Contains(next, "lines 11-20") {
		t.Fatalf("a different window was refused:\n%s", next)
	}
}

func TestAChangedFileIsReadInFullAgain(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"src/main.go": "one\ntwo\n"})
	tool := NewReadFile(root, dir)
	runTool(t, tool, `{"path":"src/main.go"}`)

	if err := os.WriteFile(filepath.Join(dir, "src/main.go"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runTool(t, tool, `{"path":"src/main.go"}`)
	if !strings.Contains(out, "lines 1-3 of 3") {
		t.Fatalf("an edited file was still pointed at:\n%s", out)
	}
}

func TestAnOutlineIsNotSentTwice(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{
		"src/main.go": "package main\n\nfunc one() {}\n\nfunc two() {}\n",
	})
	tool := NewReadFile(root, dir)

	first := runTool(t, tool, `{"path":"src/main.go","outline":true}`)
	if !strings.Contains(first, "func one") {
		t.Fatalf("the outline is missing declarations:\n%s", first)
	}
	again := runTool(t, tool, `{"path":"src/main.go","outline":true}`)
	if !strings.Contains(again, "you already have this outline") {
		t.Fatalf("the second outline came back in full:\n%s", again)
	}
}

func TestAReReadOfAForgottenResultBringsTheTextBack(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"a.txt": "one\ntwo\nthree\n"})
	tool := NewReadFile(root, dir)
	args := json.RawMessage(`{"path":"a.txt","limit":2}`)
	ctx := context.Background()

	first, err := tool.Run(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	second, err := tool.Run(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(second, "unchanged since you read") {
		t.Fatalf("the second read did not point at the first: %q", second)
	}

	tool.Forget(Shown{CallID: "r1", Text: first})
	after, err := tool.Run(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after, "one") {
		t.Errorf("a forgotten read came back as: %q", after)
	}
}

func TestAResetClearsWhatTheReaderRemembers(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"a.txt": "one\ntwo\nthree\n"})
	tool := NewReadFile(root, dir)
	args := json.RawMessage(`{"path":"a.txt","limit":2}`)
	ctx := context.Background()

	if _, err := tool.Run(ctx, args); err != nil {
		t.Fatal(err)
	}
	tool.ForgetAll()
	after, err := tool.Run(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(after, "unchanged since you read") {
		t.Errorf("after a reset the reader still pointed at a window the conversation lost: %q", after)
	}
}
