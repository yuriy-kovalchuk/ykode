package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// a write_file call cut anywhere still shows its path and the newest lines of what it writes
func TestADraftShowsAWriteBeingWritten(t *testing.T) {
	// path first, as models write it; cut mid-way through the content
	half := `{"path": "web/index.html", "content": "<html>\n\t<body>\n\t\t<p>\"hi\"</p>\n\t</bo`
	d := draft{tool: "write_file", args: half}
	if d.target() != "web/index.html" {
		t.Errorf("target = %q", d.target())
	}
	lines, total, size := d.preview()
	if total != 4 || !strings.Contains(strings.Join(lines, "\n"), "<body>") { // three written, one half-written
		t.Errorf("preview = %q (%d lines), want the written lines so far", lines, total)
	}
	if head := draftHead(d.target(), total, size); !strings.Contains(head, "web/index.html") || !strings.Contains(head, "lines") {
		t.Errorf("head = %q", head)
	}
}

// an escape the stream cut in half is dropped, not shown as a stray backslash
func TestADraftCutInsideAnEscapeShowsNoBackslash(t *testing.T) {
	d := draft{tool: "write_file", args: `{"path":"a.go","content":"x := \"a`}
	if lines, _, _ := d.preview(); strings.Join(lines, "") != `x := "a` {
		t.Errorf("preview = %q", lines)
	}
	d.args = `{"path":"a.go","content":"line one\`
	if lines, _, _ := d.preview(); strings.Join(lines, "") != "line one" {
		t.Errorf("a trailing half escape leaked: %q", lines)
	}
}

// only the newest lines are shown, however long the file grows
func TestADraftShowsOnlyTheNewestLines(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	full, _ := json.Marshal(map[string]string{"path": "big.txt", "content": b.String()})
	lines, total, size := draft{tool: "write_file", args: string(full)}.preview()
	if len(lines) != draftPreviewLines || lines[len(lines)-1] != "line 4999" {
		t.Errorf("preview = %q, want the last %d lines", lines, draftPreviewLines)
	}
	if total < 5000 || size < 30000 {
		t.Errorf("total %d lines, %d bytes: the count covers only the decoded tail", total, size)
	}
}

// a tool with no known body field shows its raw arguments
func TestADraftOfAnotherToolShowsItsArguments(t *testing.T) {
	d := draft{tool: "search_text", args: `{"pattern":"Fetch`}
	if lines, _, _ := d.preview(); len(lines) != 1 || lines[0] != `{"pattern":"Fetch` {
		t.Errorf("preview = %q", lines)
	}
}
