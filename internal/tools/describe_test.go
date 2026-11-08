package tools

import (
	"strings"
	"testing"
)

// a call is put in front of a person before they decide: the file, the pattern, the command
func TestACallSaysWhatItAsksFor(t *testing.T) {
	for _, c := range []struct{ name, args, want string }{
		{"read_file", `{"path":"src/main.go"}`, "src/main.go"},
		{"read_file", `{"path":"src/main.go","offset":40,"limit":120}`, "src/main.go:40-159"},
		{"read_file", `{"path":"main.go","outline":true}`, "main.go (outline)"},
		{"write_file", `{"path":"web/i.html","content":"one\ntwo\n"}`, "web/i.html · 2 lines · 8 B"},
		{"edit_file", `{"path":"a.go","old_text":"one\ntwo","new_text":"one"}`, "a.go · 2 lines → 1"},
		{"edit_file", `{"path":"a.go","edits":[{"old_text":"one","new_text":"two"},{"old_text":"three","new_text":"four five"}]}`, "a.go · 2 edits · 2 lines → 2"},
		{"exec_command", `{"command":"go test ./... -count=1"}`, "go test ./... -count=1"},
		{"search_text", `{"pattern":"TODO","path":"internal","glob":"*.go"}`, `"TODO" in internal *.go`},
		{"find_files", `{"pattern":"*_test.go","path":"internal"}`, `"*_test.go" under internal`},
	} {
		if got := Describe(c.name, c.args); got != c.want {
			t.Errorf("%s %s = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}

// a call this package does not know, or arguments the model cut off, still gets a row
func TestAnUnreadableCallStillGetsADescription(t *testing.T) {
	if got := Describe("write_file", `{"path":`); got == "" {
		t.Error("half-written arguments described as nothing")
	}
	if got := Describe("mystery_tool", `{"a": 1}`); got != `{"a": 1}` {
		t.Errorf("an unknown tool described as %q", got)
	}
}

// a command longer than a row is cut, and says it was
func TestALongCommandIsCutForTheRow(t *testing.T) {
	long := Describe("exec_command", `{"command":"`+strings.Repeat("echo; ", 60)+`true"}`)
	if !strings.HasSuffix(long, " […cut]") {
		t.Errorf("a long command was not marked as cut: %q", long)
	}
	if len(long) > describeChars+10 {
		t.Errorf("a cut command is still %d characters", len(long))
	}
}
