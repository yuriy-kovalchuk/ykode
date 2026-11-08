package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSearchRipgrepMatchesTheFallback(t *testing.T) {
	root, dir := searchFixture(t)
	goTool := &SearchText{scope: scope{root: root, dir: dir}}
	rgTool := NewSearchText(root, dir)
	if rgTool.rgPath == "" {
		t.Skip("ripgrep is not installed")
	}
	// Both engines must agree on which files match.
	for _, a := range []string{`{"pattern":"Needle"}`, `{"pattern":"NEEDLE","ignore_case":true}`, `{"pattern":"Needle","glob":"*.go"}`} {
		goOut := runSearch(t, goTool, a)
		rgOut := runSearch(t, rgTool, a)
		if normalizeHits(goOut) != normalizeHits(rgOut) {
			t.Errorf("args %s disagree\ngo: %s\nrg: %s", a, goOut, rgOut)
		}
	}
	bad := json.RawMessage(`{"pattern":"(Needle"}`)
	if _, err := goTool.Run(context.Background(), bad); err == nil {
		t.Error("go walker accepted an invalid regex")
	}
	if _, err := rgTool.Run(context.Background(), bad); err == nil {
		t.Error("rg accepted an invalid regex")
	}
}

func TestSearchLimitStopsEarlyOnBothEngines(t *testing.T) {
	root, dir := searchFixture(t)
	tools := []*SearchText{{scope: scope{root: root, dir: dir}}, NewSearchText(root, dir)}
	if tools[1].rgPath == "" {
		tools = tools[:1]
	}
	for _, tool := range tools {
		name := tool.Name()
		out := runSearch(t, tool, `{"pattern":"Needle","limit":2,"context":1}`)
		if !strings.Contains(out, "[stopped at the 2 match limit") {
			t.Errorf("%s: limit note missing:\n%s", name, out)
		}
		matches := 0
		for _, line := range matchLines(out) {
			if matchLine.MatchString(line) {
				matches++
			}
		}
		if matches != 2 {
			t.Errorf("%s: wanted exactly 2 matches, got %d:\n%s", name, matches, out)
		}
	}
}

// 40 files x 500 lines guarantees rg is still streaming when the limit lands (the pipe holds 64 KB).
func TestSearchLimitStopsRipgrepEarly(t *testing.T) {
	root, dir := searchFixture(t)
	line := strings.Repeat("Needle padded out so the matches fill a pipe quickly\n", 20)
	for i := 0; i < 40; i++ {
		path := filepath.Join(dir, "big", fmt.Sprintf("f%02d.txt", i))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Repeat(line, 25)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tool := NewSearchText(root, dir)
	if tool.rgPath == "" {
		t.Skip("ripgrep is not installed")
	}

	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := tool.Run(context.Background(), json.RawMessage(`{"pattern":"Needle","limit":2}`))
		done <- result{out, err}
	}()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("search at the limit failed: %v", got.err)
		}
		matches := 0
		for _, l := range matchLines(got.out) {
			if matchLine.MatchString(l) {
				matches++
			}
		}
		if matches != 2 {
			t.Fatalf("wanted 2 matches, got %d:\n%s", matches, got.out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rg kept running after the match limit was reached")
	}
}

func TestSearchSummariesMatchRipgrep(t *testing.T) {
	root, dir := searchFixture(t)
	goTool := &SearchText{scope: scope{root: root, dir: dir}}
	rgTool := NewSearchText(root, dir)
	if rgTool.rgPath == "" {
		t.Skip("ripgrep is not installed")
	}
	modes := []string{`{"pattern":"Needle","mode":"count"}`, `{"pattern":"Needle","mode":"files_with_matches"}`,
		`{"pattern":"Needle","mode":"count","glob":"*.go"}`, `{"pattern":"NEEDLE","mode":"count","ignore_case":true}`}
	for _, a := range modes {
		if sortedLines(runSearch(t, goTool, a)) != sortedLines(runSearch(t, rgTool, a)) {
			t.Errorf("args %s disagree\ngo: %s\nrg: %s", a, runSearch(t, goTool, a), runSearch(t, rgTool, a))
		}
	}
}

func TestSearchRipgrepSummaryDoesNotInventAFileCount(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{
		"f1.go": "needle\n", "f2.go": "needle\n", "f3.go": "needle\n", "f4.go": "needle\n",
	})
	rgTool := NewSearchText(root, dir)
	if rgTool.rgPath == "" {
		t.Skip("ripgrep is not installed")
	}
	got := runSearch(t, rgTool, `{"pattern":"needle","mode":"count","limit":2}`)
	if strings.Contains(got, "after 0 files") {
		t.Errorf("ripgrep never reported a scanned count, so the note must not claim one:\n%s", got)
	}
	if !strings.Contains(got, "limit") {
		t.Errorf("a capped summary must say so:\n%s", got)
	}
}
