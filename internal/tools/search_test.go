package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// searchFixture covers the awkward cases: ignored dirs, negation, nested .gitignore, binary, big file.
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
	// several matches in one file share a header, context lines included
	if !strings.Contains(out, "internal/a.go:\n  2-") || !strings.Contains(out, "  3:") || !strings.Contains(out, "  4-") {
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

// summaryFixture has three matches in two files, and one file with none.
func summaryFixture(t *testing.T) (*SearchText, string) {
	t.Helper()
	root, dir := treeFixture(t, map[string]string{
		"a.go":     "x\nneedle\nneedle\ny\n",
		"sub/b.go": "needle\n",
		"c.go":     "nothing here\n",
	})
	return &SearchText{scope: scope{root: root, dir: dir}}, dir
}

func TestSearchCountModeTotalsPerFile(t *testing.T) {
	tool, _ := summaryFixture(t)
	got := runSearch(t, tool, `{"pattern":"needle","mode":"count"}`)
	for _, want := range []string{"a.go:2", "sub/b.go:1", "3 matches in 2 files"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "c.go") {
		t.Errorf("a file with no matches must not be listed:\n%s", got)
	}
}

func TestSearchFilesModeListsEachFileOnce(t *testing.T) {
	tool, _ := summaryFixture(t)
	got := runSearch(t, tool, `{"pattern":"needle","mode":"files_with_matches"}`)
	if strings.Count(got, "a.go") != 1 {
		t.Errorf("a file with two matches must appear once:\n%s", got)
	}
	if !strings.Contains(got, "sub/b.go") || !strings.Contains(got, "2 files matched") {
		t.Errorf("want both files and a count, got:\n%s", got)
	}
	if strings.Contains(got, "needle") || strings.Contains(got, "a.go:") || strings.Contains(got, "sub/b.go:") {
		t.Errorf("this mode must not echo matched text or print counts:\n%s", got)
	}
}

func TestSearchSummaryHonoursTheFileLimit(t *testing.T) {
	tool, _ := summaryFixture(t)
	got := runSearch(t, tool, `{"pattern":"needle","mode":"count","limit":1}`)
	if !strings.Contains(got, "limit") {
		t.Errorf("a capped summary must say so:\n%s", got)
	}
}

func TestSearchRejectsAnUnknownMode(t *testing.T) {
	tool, _ := summaryFixture(t)
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"pattern":"needle","mode":"sideways"}`)); err == nil ||
		!strings.Contains(err.Error(), "files_with_matches") {
		t.Errorf("err = %v, want the valid modes named", err)
	}
}

func sortedLines(out string) string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func TestSettingsSetTheMatchLimit(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"hits.txt": "match\nmatch\nmatch\nmatch\n"})
	tool := NewSearchText(root, dir)
	tool.MatchLimit = 2

	out := runTool(t, tool, `{"pattern":"match"}`)
	if got := strings.Count(out, ":match"); got != 2 {
		t.Fatalf("%d matches shown, want the configured limit of 2:\n%s", got, out)
	}
}

func TestSeveralMatchesInOneFileShareAHeader(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{
		"src/main.go": "needle one\nneedle two\nneedle three\nother\n",
		"src/two.go":  "needle four\n",
	})
	tool := &SearchText{scope: scope{root: root, dir: dir}}

	out := runSearch(t, tool, `{"pattern":"needle"}`)
	if got := strings.Count(out, "src/main.go"); got != 1 {
		t.Fatalf("the path appears %d times, want once as a header:\n%s", got, out)
	}
	for _, want := range []string{"src/main.go:\n  1:", "  2:", "  3:", "src/two.go:1:"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from:\n%s", want, out)
		}
	}
}

func TestContextLinesShareTheFileHeaderToo(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{
		"src/main.go": "before\nneedle one\nbetween\nneedle two\nend\n",
	})
	goOnly := &SearchText{scope: scope{root: root, dir: dir}}

	out := runSearch(t, goOnly, `{"pattern":"needle","context":1}`)
	if got := strings.Count(out, "src/main.go"); got != 1 {
		t.Fatalf("the path appears %d times, want it once as a header:\n%s", got, out)
	}
	for _, want := range []string{"src/main.go:\n  1-", "  2:", "  3-", "  4:", "  5-"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from:\n%s", want, out)
		}
	}
	if both := NewSearchText(root, dir); both.rgPath != "" {
		if rgOut := runSearch(t, both, `{"pattern":"needle","context":1}`); rgOut != out {
			t.Errorf("the two search paths disagree:\n%s\n---\n%s", out, rgOut)
		}
	}
}
