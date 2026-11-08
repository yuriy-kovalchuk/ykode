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
	d := drafted(newDraft(1, "write_file", half))
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
	d := drafted(newDraft(1, "write_file", `{"path":"a.go","content":"x := \"a`))
	if lines, _, _ := d.preview(); strings.Join(lines, "") != `x := "a` {
		t.Errorf("preview = %q", lines)
	}
	cut := drafted(newDraft(1, "write_file", `{"path":"a.go","content":"line one\`))
	if lines, _, _ := cut.preview(); strings.Join(lines, "") != "line one" {
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
	lines, total, size := drafted(newDraft(1, "write_file", string(full))).preview()
	if len(lines) != draftPreviewLines || lines[len(lines)-1] != "line 4999" {
		t.Errorf("preview = %q, want the last %d lines", lines, draftPreviewLines)
	}
	if total < 5000 || size < 30000 {
		t.Errorf("total %d lines, %d bytes: the count covers only the decoded tail", total, size)
	}
}

// a tool with no known body field shows its raw arguments
func TestADraftOfAnotherToolShowsItsArguments(t *testing.T) {
	d := drafted(newDraft(1, "search_text", `{"pattern":"Fetch`))
	if lines, _, _ := d.preview(); len(lines) != 1 || lines[0] != `{"pattern":"Fetch` {
		t.Errorf("preview = %q", lines)
	}
}

// the numbers a call being written reports are walked a piece at a time: a line break split between
// two pieces is counted once, and nothing is walked again once the text is written
func TestACallBeingWrittenKeepsItsCountAsPiecesArrive(t *testing.T) {
	e := newDraft(1, "write_file", `{"path":"a.go","content":"one`)
	if _, lines, _ := drafted(e).preview(); lines != 1 {
		t.Errorf("before any line break: %d lines, want 1", lines)
	}
	e.args += `\`
	if _, lines, _ := drafted(e).preview(); lines != 1 {
		t.Errorf("a line break cut in half was counted early: %d, want 1", lines)
	}
	e.args += `ntwo"`
	_, lines, bytes := drafted(e).preview()
	if lines != 2 {
		t.Errorf("after the break arrived: %d lines, want 2", lines)
	}
	if bytes != len(`one\ntwo`) {
		t.Errorf("%d bytes, want the text written (%d)", bytes, len(`one\ntwo`))
	}

	e.args += `,"another":"field"}`
	if _, lines, bytes := drafted(e).preview(); lines != 2 || bytes != len(`one\ntwo`) {
		t.Errorf("what the model wrote after the text stopped changed its numbers: %d lines, %d bytes", lines, bytes)
	}
}

// what a call names as its path is settled before the file's own text begins, so a file that talks
// about a path field cannot be mistaken for the call's own
func TestACallNamesItsPathNotWhatTheFileSays(t *testing.T) {
	e := newDraft(1, "write_file", `{"path":"a.go","content":"// the settings say `)
	if got := drafted(e).target(); got != "a.go" {
		t.Fatalf("target = %q, want a.go", got)
	}
	e.args += `"path": "b.go"` + `"}`
	if got := drafted(e).target(); got != "a.go" {
		t.Errorf("the file's own text became the target: %q", got)
	}
}

// a model writing a second edit of the same file gets the preview of the text it is writing now
func TestThePreviewFollowsTheNewestTextBeingWritten(t *testing.T) {
	e := newDraft(1, "edit_file", `{"path":"a.go","edits":[{"old_text":"one","new_text":"first line"}]}`)
	if lines, _, _ := drafted(e).preview(); len(lines) != 1 || lines[0] != "first line" {
		t.Fatalf("preview = %q, want the first edit's text", lines)
	}
	e.args += `,{"old_text":"two","new_text":"second line"}]}`
	if lines, _, _ := drafted(e).preview(); len(lines) != 1 || lines[0] != "second line" {
		t.Errorf("the preview stayed on the finished edit: %q", lines)
	}
}
