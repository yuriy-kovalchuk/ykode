package tools

import (
	"context"
	"encoding/json"
	"fmt"
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
	if !strings.Contains(out, "2\tline 2") || !strings.Contains(out, "3\tline 3") {
		t.Fatalf("window wrong:\n%s", out)
	}
	if !strings.Contains(out, "[showing lines 2-3 of 5 (line limit); continue with offset=4]") {
		t.Fatalf("marker wrong:\n%s", out)
	}
}

func TestReadCutsByBytesBeforeTheLineLimit(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 1000; i++ {
		fmt.Fprintf(&b, "line %04d %s\n", i, strings.Repeat("x", 60))
	}
	tool := readToolFixture(t, b.String())

	out, err := tool.Run(context.Background(), json.RawMessage(`{"path":"f.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > maxResultBytes {
		t.Fatalf("result is %d bytes, over the %d byte cap", len(out), maxResultBytes)
	}
	if !strings.Contains(out, fmt.Sprintf("(%d byte limit); continue with offset=", resultBudget)) {
		t.Fatalf("marker wrong:\n%s", cutUTF8(out, 200))
	}
	if lines := strings.Count(strings.TrimSpace(out), "\n"); lines < 100 || lines >= 1000 {
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
	if !strings.Contains(out, "(line limit); continue with offset=401]") {
		t.Fatalf("marker wrong:\n%s", cutUTF8(out, 200))
	}
}

func TestReadRefusesADirectoryWithAHint(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"src/main.go": "package main\n"})
	_, err := NewReadFile(root, dir).Run(context.Background(), json.RawMessage(`{"path":"src"}`))
	if err == nil || !strings.Contains(err.Error(), "list_dir") {
		t.Errorf("err = %v, want it to point at list_dir", err)
	}
}
