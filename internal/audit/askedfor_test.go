package audit

import (
	"strings"
	"testing"
)

// Arguments read as rows in the order the model wrote them, each labelled: reading a call is about the
// command and the flags that change it, not about the order the alphabet puts them in.
func TestArgumentsReadAsRowsInTheOrderTheyWereWritten(t *testing.T) {
	rows := AskedFor(`{"command":"go vet ./internal","timeout":240,"dir":"internal/audit"}`)
	want := []string{"command: go vet ./internal", "timeout: 240", "dir: internal/audit"}
	if len(rows) != len(want) || strings.Join(rows, " | ") != strings.Join(want, " | ") {
		t.Errorf("the call reads:\n  %v\nwant:\n  %v", rows, want)
	}
}

// A command longer than a row is still written out whole: a cut command is not a shorter command, it is a
// wrong one, and whoever shows the row can wrap it.
func TestALongCommandIsNotCut(t *testing.T) {
	long := "go test ./internal/tui/ -run 'History' -v 2>&1 | grep -E '^--- FAIL' | head -20"
	rows := AskedFor(`{"command":"` + long + `"}`)
	if len(rows) != 1 || rows[0] != "command: "+long {
		t.Errorf("a long command reads %v", rows)
	}
}

// A value spanning lines is counted with its first line, because a search hit or a file's content is
// otherwise a wall nobody reads; the whole of it belongs to the view of one call.
func TestAValueSpanningLinesIsCountedWhereItIsListed(t *testing.T) {
	args := `{"path":"a.go","content":"short file\nsecond line\nthird line"}`
	rows := AskedFor(args)
	if rows[0] != "path: a.go" || !strings.HasPrefix(rows[1], "content: short file · 3 lines · ") {
		t.Errorf("a block reads %v", rows)
	}
	if strings.Contains(strings.Join(rows, " "), "second line") {
		t.Errorf("the listed form quoted the whole block: %v", rows)
	}

	// ...and written out where there is room for it
	if whole := WholeArgs(`{"content":"short file\nsecond line\nthird line"}`); len(whole) != 3 ||
		whole[0] != "content: short file" || whole[1] != "  second line" || whole[2] != "  third line" {
		t.Errorf("the whole form reads %v", whole)
	}
}

// Several things in one argument are counted, and named when naming them is shorter than counting them.
func TestSeveralThingsInOneArgument(t *testing.T) {
	rows := AskedFor(`{"glob":"*.go *_test.go","edits":[{"old_text":"a","new_text":"b"},{"old_text":"c","new_text":"d"}]}`)
	if rows[0] != "glob: *.go *_test.go" || rows[1] != "edits: 2 parts · 65 B" {
		t.Errorf("several arguments read %v", rows)
	}
}

// A log line written before the arguments were kept whole holds a fragment cut mid-value; the pairs that
// survived are still worth reading, and neither half of the answer is a brace.
func TestACutFragmentIsStillRead(t *testing.T) {
	rows := AskedFor(`{"path":"main.go","command":"go build ./... && go test ./internal/…`)
	if len(rows) < 2 || rows[0] != "path: main.go" || rows[1] != "command: go build ./... && go test ./internal/…" {
		t.Errorf("a cut fragment reads %v", rows)
	}
	if empty := AskedFor(""); empty != nil {
		t.Errorf("no arguments read %v", empty)
	}
}

// A size says itself in the units a person expects: a search that hit 600 KB is not 614400 B.
func TestSizesSayTheirOwnUnits(t *testing.T) {
	for _, step := range []struct {
		in   int
		want string
	}{{900, "900 B"}, {2048, "2.0 KB"}, {614400, "600.0 KB"}} {
		if got := bytes(step.in); got != step.want {
			t.Errorf("%d reads %q, want %q", step.in, got, step.want)
		}
	}
}
