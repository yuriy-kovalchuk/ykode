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
	if len(got) > maxResultBytes {
		t.Fatalf("result is %d bytes, over the cap of %d", len(got), maxResultBytes)
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
