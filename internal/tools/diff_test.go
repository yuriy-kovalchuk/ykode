package tools

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestEditResultShowsTheChange(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"a.txt": "one\ntwo\nthree\nfour\nfive\nsix\nseven\n"})
	got := runTool(t, NewEditFile(root, dir), `{"path":"a.txt","edits":[{"old_text":"three","new_text":"THREE"}]}`)
	for _, want := range []string{"-three", "+THREE", " two", " four"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "-one") || strings.Contains(got, "+seven") {
		t.Errorf("diff shows untouched far-away lines:\n%s", got)
	}
}

func TestEditResultShowsEveryHunk(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	root, dir := treeFixture(t, map[string]string{"a.txt": sb.String()})
	got := runTool(t, NewEditFile(root, dir), `{"path":"a.txt","edits":[
		{"old_text":"line 0\n","new_text":"ALPHA\n"},
		{"old_text":"line 30\n","new_text":"BETA\n"}]}`)
	if n := strings.Count(got, "@@"); n < 2 {
		t.Errorf("want a hunk per change, got %d in:\n%s", n, got)
	}
	if !strings.Contains(got, "-line 0") || !strings.Contains(got, "-line 30") {
		t.Errorf("both edits must appear:\n%s", got)
	}
}

func TestALargeChangeIsSummarisedInsteadOfPrinted(t *testing.T) {
	got := diffText(numbered(4000, "old"), numbered(4000, "new"))
	if !strings.Contains(got, "large change") || !strings.Contains(got, "read_file") {
		t.Errorf("want a pointer to read_file, got: %s", got)
	}
	if len(got) > 200 {
		t.Errorf("the summary should be short, got %d bytes", len(got))
	}
}

func TestAWideDiffIsCutWithAHint(t *testing.T) {
	got := diffText(padded(120, "x"), padded(120, "y"))
	if len(got) > diffMaxBytes+len("read_file to see the whole result]")+64 {
		t.Errorf("diff is %d bytes, over its own cap of %d", len(got), diffMaxBytes)
	}
	if !strings.Contains(got, "read_file") {
		t.Errorf("a cut diff must say how to see the rest:\n%s", got[:200])
	}
}

func TestEditResultStaysInsideTheToolBudget(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"wide.txt": padded(120, "x")})
	got := runTool(t, NewEditFile(root, dir), fmt.Sprintf(
		`{"path":"wide.txt","edits":[{"old_text":%s,"new_text":%s}]}`,
		jsonQuote(padded(120, "x")), jsonQuote(padded(120, "y"))))
	if len(got) > defaultResultBytes {
		t.Fatalf("result is %d bytes, over the cap of %d", len(got), defaultResultBytes)
	}
}

func numbered(n int, word string) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString(word + " " + strconv.Itoa(i) + "\n")
	}
	return sb.String()
}

func padded(n int, mark string) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString(mark + strings.Repeat(".", 90) + strconv.Itoa(i) + "\n")
	}
	return sb.String()
}

func jsonQuote(s string) string {
	data, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func TestWriteOverwriteShowsWhatChanged(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"a.txt": "alpha\nbeta\ngamma\n"})
	got := runTool(t, NewWriteFile(root, dir), `{"path":"a.txt","content":"alpha\nBETA\ngamma\n"}`)
	if !strings.Contains(got, "-beta") || !strings.Contains(got, "+BETA") {
		t.Errorf("overwrite should show a diff:\n%s", got)
	}
}

func TestWriteANewFileReportsCreationInsteadOfADiff(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{})
	got := runTool(t, NewWriteFile(root, dir), `{"path":"new/a.txt","content":"one\ntwo\n"}`)
	if !strings.Contains(got, "created") {
		t.Errorf("want a creation notice, got: %s", got)
	}
	if strings.Contains(got, "@@") {
		t.Errorf("a new file has no diff to show:\n%s", got)
	}
}

func TestADiffCarriesNoFileHeaders(t *testing.T) {
	// the tool result already names the file; "--- before / +++ after" is tokens for nothing
	diff := diffText("one\ntwo\n", "one\nTWO\n")
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			t.Errorf("a file header reached the model in:\n%s", diff)
		}
	}
	if !strings.HasPrefix(diff, "@@") {
		t.Errorf("a diff should start at a hunk:\n%s", diff)
	}
}

func TestADiffOfACRLFFileStaysSmall(t *testing.T) {
	// a CRLF file whose \r is not normalised looks like every line changed
	tool, current := editToolFixture(t, "one\r\ntwo\r\nthree\r\n")
	args := []byte(`{"path":"target.txt","edits":[{"old_string":"two","new_string":"TWO"}]}`)
	out, err := tool.Run(t.Context(), args)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "large change") {
		t.Errorf("one line changed but the diff gave up: %s", out)
	}
	if !strings.Contains(out, "-two") || !strings.Contains(out, "+TWO") {
		t.Errorf("the change is not in the diff: %s", out)
	}
	if strings.Contains(current(), "\n\r") {
		t.Errorf("the file was mangled: %q", current())
	}
}

func TestOverwritingACRLFFileShowsOneChangeNotEveryLine(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"notes.txt": "one\r\ntwo\r\nthree\r\n"})
	write, ok := BuiltIn(root, dir, Options{MaxResultBytes: 4000}).Get("write_file")
	if !ok {
		t.Fatal("no write_file")
	}
	out, err := write.Run(t.Context(), []byte(`{"path":"notes.txt","content":"one\nTWO\nthree\n"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "large change") {
		t.Errorf("three lines, one changed, diff gave up: %s", out)
	}
	if !strings.Contains(out, "-two") || !strings.Contains(out, "+TWO") {
		t.Errorf("the change is not in the diff: %s", out)
	}
	var changed int
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "+") {
			changed++
		}
	}
	if changed != 2 {
		t.Errorf("%d lines in the diff for a one-line change (the line endings were not normalised):\n%s", changed, out)
	}
}

// the screen is handed the whole change, however much of it the model's own copy leaves out
func TestTheWholeChangeIsNotCapped(t *testing.T) {
	var before, after strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&before, "line %d of the file, long enough to count\n", i)
		fmt.Fprintf(&after, "line %d of the file, long enough to count\n", i)
		if i%5 == 0 {
			fmt.Fprintf(&after, "a line added after line %d\n", i)
		}
	}
	short, whole := diffText(before.String(), after.String()), wholeChange(before.String(), after.String())
	if got := strings.Count(whole, "\n+a line added"); got != 80 {
		t.Errorf("the whole change holds %d of the 80 added lines", got)
	}
	if len(short) >= len(whole) {
		t.Errorf("the model's copy (%d bytes) is not shorter than the screen's (%d)", len(short), len(whole))
	}
}

// a file rewritten whole is given as what went and what came, without lining the two up, and a
// change past what a screen is handed says how much it left out
func TestAFileRewrittenWholeIsStillAChange(t *testing.T) {
	var before, after strings.Builder
	for i := 0; i < changeMaxLines; i++ {
		fmt.Fprintf(&before, "old %d\n", i)
		fmt.Fprintf(&after, "new %d\n", i)
	}
	whole := wholeChange("package x\n"+before.String(), "package x\n"+after.String())
	if !strings.HasPrefix(whole, fmt.Sprintf("@@ -2,%d +2,%d @@\n-old 0\n", changeMaxLines, changeMaxLines)) {
		t.Errorf("the change does not start where the files part: %.80q", whole)
	}
	if !strings.Contains(whole, "more lines of the change") || strings.Count(whole, "\n") != changeMaxLines {
		t.Errorf("a change past the limit is %d lines and does not say what it left out", strings.Count(whole, "\n"))
	}
}
