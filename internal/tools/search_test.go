package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// searchFixture is a tree with the awkward cases: gitignored directories, a
// negated pattern, a nested .gitignore that overrides the outer one, a binary
// file, and an untracked large file.
func searchFixture(t *testing.T) (*os.Root, string) {
	t.Helper()
	files := map[string]string{
		"main.go":            "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n",
		"internal/a.go":      "package a\n\n// Needle in Go\nfunc A() {}\n",
		"internal/b.txt":     "Needle in a text file\n",
		"node_modules/x.go":  "Needle in node_modules\n",
		"ignored/keep.go":    "Needle in an ignored directory\n",
		"important.log":      "Needle in an un-ignored log\n",
		"sub/notes.log":      "Needle allowed by the nested gitignore\n",
		"sub/plain.txt":      "nothing here\n",
		"big.log":            "Needle in an ignored log\n",
		"data.bin":           "Needle\x00binary\n",
		".gitignore":         "node_modules/\nignored/\n*.log\n!important.log\n",
		"sub/.gitignore":     "!*.log\n",
		"extra/untracked.go": "Needle in a huge directory\n",
	}
	root, dir := treeFixture(t, files)
	// .git is skipped without a .gitignore rule, like every search tool does.
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("Needle in .git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, dir
}

func runSearch(t *testing.T, tool *SearchText, args string) string {
	t.Helper()
	out, err := tool.Run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("search_text %s: %v", args, err)
	}
	return out
}

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

// normalizeHits drops ordering and notes so only the matched files compare.
func normalizeHits(out string) string {
	var kept []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		if i := strings.Index(line, ":"); i > 0 {
			kept = append(kept, line[:i])
		}
	}
	slices.Sort(kept)
	return strings.Join(kept, "\n")
}

func matchLines(out string) []string {
	var kept []string
	for _, line := range strings.Split(out, "\n") {
		if line != "" && !strings.HasPrefix(line, "[") {
			kept = append(kept, line)
		}
	}
	return kept
}

func TestSearchLiteralAndContext(t *testing.T) {
	root, dir := searchFixture(t)
	tool := &SearchText{scope: scope{root: root, dir: dir}}

	out := runSearch(t, tool, `{"pattern":"// Needle in Go","literal":true,"context":1}`)
	if !strings.Contains(out, "internal/a.go-2-") || !strings.Contains(out, "internal/a.go:3:") || !strings.Contains(out, "internal/a.go-4-") {
		t.Fatalf("context lines missing:\n%s", out)
	}
	if out2 := runSearch(t, tool, `{"pattern":"println(\"hello\")","literal":true}`); !strings.Contains(out2, "main.go:4:") {
		t.Fatalf("literal regex characters should not be special:\n%s", out2)
	}
}

func TestSearchLimitAndBadPattern(t *testing.T) {
	root, dir := searchFixture(t)
	tool := &SearchText{scope: scope{root: root, dir: dir}}

	out := runSearch(t, tool, `{"pattern":"Needle","limit":2}`)
	if !strings.Contains(out, "[stopped at the 2 match limit") {
		t.Fatalf("limit note missing:\n%s", out)
	}
	if got := len(matchLines(out)); got != 2 {
		t.Fatalf("limit 2 respected? got %d matches:\n%s", got, out)
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"pattern":"(Needle"}`)); err == nil ||
		!strings.Contains(err.Error(), "bad pattern") {
		t.Fatalf("bad regex error = %v, want it to name the pattern", err)
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"pattern":""}`)); err == nil ||
		!strings.Contains(err.Error(), "pattern is empty") {
		t.Fatalf("empty pattern error = %v", err)
	}
}

func TestSearchSingleFileAndNoMatches(t *testing.T) {
	root, dir := searchFixture(t)
	tool := &SearchText{scope: scope{root: root, dir: dir}}

	if out := runSearch(t, tool, `{"pattern":"Needle","path":"internal/b.txt"}`); !strings.Contains(out, "internal/b.txt:1:") {
		t.Fatalf("searching one file:\n%s", out)
	}
	if out := runSearch(t, tool, `{"pattern":"zzz-nothing"}`); out != "(no matches)" {
		t.Fatalf("no matches = %q", out)
	}
}

func TestSearchOutsideRootNeedsApproval(t *testing.T) {
	root, dir := searchFixture(t)
	parent := filepath.Dir(dir)
	tool := &SearchText{scope: scope{root: root, dir: dir}}

	targets, err := tool.Targets(json.RawMessage(`{"pattern":"x","path":".."}`))
	if err != nil || len(targets) != 1 || !targets[0].Outside {
		t.Fatalf("targets = %v, %v; want one outside target", targets, err)
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"pattern":"Needle","path":".."}`)); err == nil ||
		!strings.Contains(err.Error(), "was not approved") {
		t.Fatalf("unapproved outside search error = %v", err)
	}
	ctx := WithApprovals(context.Background(), []string{parent})
	out, err := tool.Run(ctx, json.RawMessage(`{"pattern":"Needle in Go","literal":true,"path":".."}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, filepath.Join(dir, "internal", "a.go")) {
		t.Fatalf("approved outside search should name absolute paths:\n%s", out)
	}
}

var matchLine = regexp.MustCompile(`^[^:]+:[0-9]+:`)

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

// TestSearchLimitStopsRipgrepEarly covers the case where rg is still producing
// output when the limit is reached: the pipe holds 64 KB, so 40 files x 500
// lines guarantees rg is mid-stream.
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
