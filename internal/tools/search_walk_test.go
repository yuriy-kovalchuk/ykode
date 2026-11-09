package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

// a single file named by the search is refused when it is bigger than the tool reads, rather than
// pulled into RAM whole
func TestANamedFilePastTheScanLimitIsRefused(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"small.go": "needle\n"})
	big := bytes.Repeat([]byte("a line of a big log file\n"), 200_000) // ~4 MB
	if err := os.WriteFile(filepath.Join(dir, "huge.log"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	tool := &SearchText{scope: scope{root: root, dir: dir}} // rgPath empty forces the Go walker
	_, err := tool.Run(context.Background(), json.RawMessage(`{"pattern":"needle","path":"huge.log"}`))
	if err == nil || !strings.Contains(err.Error(), "exec_command") {
		t.Errorf("a 4 MB file was searched instead of refused: %v", err)
	}
}
