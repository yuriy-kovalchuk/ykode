package tools

import (
	"strconv"
	"strings"
	"testing"
)

func TestSearchGoFindsFilesAndHonoursGitignore(t *testing.T) {
	root, dir := searchFixture(t)
	tool := &SearchText{scope: scope{root: root, dir: dir}} // rgPath empty forces the Go walker
	out := runSearch(t, tool, `{"pattern":"Needle"}`)

	for _, want := range []string{
		"internal/a.go:3:", "internal/b.txt:1:", "important.log:1:",
		"sub/notes.log:1:", "extra/untracked.go:1:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("result missing %s\n%s", want, out)
		}
	}
	for _, notWant := range []string{
		"node_modules", "ignored/keep.go", "big.log", "data.bin", ".git/",
	} {
		if strings.Contains(out, notWant) {
			t.Errorf("result should skip %s\n%s", notWant, out)
		}
	}
}

func TestSearchSummaryStopsScanningAtTheFileLimit(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 30; i++ {
		files["f"+strconv.Itoa(i)+".go"] = "needle\n"
	}
	root, dir := treeFixture(t, files)
	tool := &SearchText{scope: scope{root: root, dir: dir}}
	got := runSearch(t, tool, `{"pattern":"needle","mode":"count","limit":2}`)
	if !strings.Contains(got, "after 2 files;") {
		t.Errorf("the walker must stop at the limit, not scan the tree:\n%s", got)
	}
}
