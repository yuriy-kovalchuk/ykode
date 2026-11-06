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

func listToolFixture(t *testing.T, files, dirs []string) *ListDir {
	t.Helper()
	tree := make(map[string]string, len(files))
	for _, name := range files {
		tree[name] = "x"
	}
	root, dir := treeFixture(t, tree)
	for _, name := range dirs {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return NewListDir(root, dir)
}

func listDir(t *testing.T, tool *ListDir, path string) string {
	t.Helper()
	out, err := tool.Run(context.Background(), json.RawMessage(fmt.Sprintf(`{"path":%q}`, path)))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestListDirLabelsEntriesAndEmptyDirs(t *testing.T) {
	tool := listToolFixture(t, []string{"beta.txt", "alpha.go"}, []string{"internal"})
	got := listDir(t, tool, ".")
	if got != "alpha.go\nbeta.txt\ninternal/\n" {
		t.Fatalf("listing = %q, want names sorted with dirs marked", got)
	}
	nested := listToolFixture(t, nil, nil)
	if out := listDir(t, nested, "."); out != "(empty directory)" {
		t.Fatalf("empty listing = %q", out)
	}
}

func TestListDirMarksTruncatedEntries(t *testing.T) {
	files := make([]string, 0, maxListEntries+50)
	for i := 0; i < maxListEntries+50; i++ {
		files = append(files, fmt.Sprintf("f%03d.go", i))
	}
	tool := listToolFixture(t, files, nil)

	got := listDir(t, tool, ".")
	if lines := strings.Count(got, "\n"); lines != maxListEntries+1 {
		t.Fatalf("got %d lines, want %d entries plus the marker", lines, maxListEntries)
	}
	want := fmt.Sprintf("[... 50 more entries of %d;", maxListEntries+50)
	if !strings.Contains(got, want) {
		t.Fatalf("marker %q missing:\n%s", want, cutUTF8(got, 200))
	}
	if !strings.Contains(got, "find_files") {
		t.Fatal("the cut must say how to continue")
	}
}

func TestListDirStaysInsideTheByteBudget(t *testing.T) {
	long := strings.Repeat("n", 100)
	files := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		files = append(files, fmt.Sprintf("%s%03d", long, i))
	}
	tool := listToolFixture(t, files, nil)

	got := listDir(t, tool, ".")
	if len(got) > maxResultBytes {
		t.Fatalf("result is %d bytes, over the %d cap", len(got), maxResultBytes)
	}
	if !strings.Contains(got, "more entries of 200;") {
		t.Fatalf("no truncation marker in a %d byte listing", len(got))
	}
}

func TestListDirRefusesAFileWithAHint(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"a.go": "package a\n"})
	_, err := NewListDir(root, dir).Run(context.Background(), json.RawMessage(`{"path":"a.go"}`))
	if err == nil || !strings.Contains(err.Error(), "read_file") {
		t.Errorf("err = %v, want it to point at read_file", err)
	}
}
