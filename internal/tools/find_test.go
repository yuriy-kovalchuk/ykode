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

func TestFindFilesGoWalker(t *testing.T) {
	root, dir := searchFixture(t)
	tool := &FindFiles{scope: scope{root: root, dir: dir}} // fdPath empty forces the Go walker

	cases := []struct {
		pattern string
		want    []string
		deny    []string
	}{
		{"*.go", []string{"main.go", "internal/a.go"}, []string{"node_modules/x.go", "ignored/keep.go"}},
		{"internal/**/*.go", []string{"internal/a.go"}, []string{"main.go"}},
		{"**/*.txt", []string{"internal/b.txt", "sub/plain.txt"}, nil},
		{"sub/*.txt", []string{"sub/plain.txt"}, []string{"internal/b.txt"}},
		{"*.log", []string{"important.log", "sub/notes.log"}, []string{"big.log"}},
		{"nothing.*", nil, nil},
	}
	for _, c := range cases {
		out, err := tool.Run(context.Background(), json.RawMessage(`{"pattern":`+jsonString(c.pattern)+`}`))
		if err != nil {
			t.Fatalf("%q: %v", c.pattern, err)
		}
		for _, want := range c.want {
			if !strings.Contains(out, want+"\n") {
				t.Errorf("%q missing %s:\n%s", c.pattern, want, out)
			}
		}
		for _, deny := range c.deny {
			if strings.Contains(out, deny+"\n") {
				t.Errorf("%q should not list %s:\n%s", c.pattern, deny, out)
			}
		}
		if c.want == nil && out != "(no files matched)" {
			t.Errorf("%q = %q, want no files matched", c.pattern, out)
		}
	}
}

func TestFindFilesFdMatchesTheFallback(t *testing.T) {
	root, dir := searchFixture(t)
	goTool := &FindFiles{scope: scope{root: root, dir: dir}}
	fdTool := NewFindFiles(root, dir)
	if fdTool.fdPath == "" {
		t.Skip("fd is not installed")
	}
	for _, pattern := range []string{"*.go", "internal/**/*.go", "**/*.txt", "*.log"} {
		args := json.RawMessage(`{"pattern":` + jsonString(pattern) + `}`)
		goOut, err := goTool.Run(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		fdOut, err := fdTool.Run(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(goOut) != strings.TrimSpace(fdOut) {
			t.Errorf("%q disagrees\ngo:\n%s\nfd:\n%s", pattern, goOut, fdOut)
		}
	}
}

func TestFindFilesLimitAndBadInput(t *testing.T) {
	root, dir := searchFixture(t)
	tool := &FindFiles{scope: scope{root: root, dir: dir}}

	out, err := tool.Run(context.Background(), json.RawMessage(`{"pattern":"**/*","limit":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[stopped at the 2 result limit") {
		t.Fatalf("limit note missing:\n%s", out)
	}
	if got := len(matchLines(out)); got != 2 {
		t.Fatalf("limit 2 respected? got %d:\n%s", got, out)
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"pattern":"  "}`)); err == nil {
		t.Error("empty pattern accepted")
	}
}

func TestCompileGlobMatchesWholePaths(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"*.go", "internal/deep/a.go", true},
		{"*.go", "a.txt", false},
		{"internal/*.go", "internal/a.go", true},
		{"internal/*.go", "internal/deep/a.go", false},
		{"internal/**/*.go", "internal/deep/a.go", true},
		{"?.go", "a.go", true},
		{"a[bc].go", "ab.go", true},
		{"a[bc].go", "ad.go", false},
		{"./main.go", "main.go", true},
		{"src/*.go", "internal/src/a.go", true},
		{"src/*.go", "src/a.go", true},
		{"src/*.go", "src/deep/a.go", false},
		{"**/gen/*.go", "a/gen/x.go", true},
	}
	for _, c := range cases {
		g, err := newGlob(c.pattern)
		if err != nil {
			t.Fatalf("%q: %v", c.pattern, err)
		}
		if got := g.match(c.path); got != c.want {
			t.Errorf("%q vs %q = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

// jsonString quotes a value for embedding in a JSON argument literal.
func jsonString(s string) string { return strconv.Quote(s) }

// listing must see what fd sees: binaries and oversized files included
func TestFindListsBinaryAndOversizedFiles(t *testing.T) {
	root, dir := searchFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "huge.bin"), bytes.Repeat([]byte("x"), maxScannedFileBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	finder := &FindFiles{scope: scope{root: root, dir: dir}}
	out, err := finder.Run(context.Background(), json.RawMessage(`{"pattern":"**/*.bin"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"data.bin", "huge.bin"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing %s:\n%s", want, out)
		}
	}
	searcher := &SearchText{scope: scope{root: root, dir: dir}}
	if got := runSearch(t, searcher, `{"pattern":"xxxx","literal":true}`); strings.Contains(got, "huge.bin") {
		t.Errorf("search should skip the oversized file:\n%s", got)
	}
}

func TestDepthOneListsOneDirectory(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{
		"top.go":        "package top\n",
		"src/main.go":   "package main\n",
		"src/deep/x.go": "package deep\n",
		"src/asset.png": "binary",
	})
	tool := &FindFiles{scope: scope{root: root, dir: dir}}

	out := runTool(t, tool, `{"pattern":"*","path":"src","depth":1}`)
	if !strings.Contains(out, "src/main.go") || !strings.Contains(out, "src/asset.png") {
		t.Fatalf("files of src missing:\n%s", out)
	}
	if !strings.Contains(out, "src/deep/") {
		t.Fatalf("the subdirectory is not marked as a directory:\n%s", out)
	}
	if strings.Contains(out, "src/deep/x.go") || strings.Contains(out, "top.go") {
		t.Fatalf("depth 1 reached past the first level:\n%s", out)
	}
}
