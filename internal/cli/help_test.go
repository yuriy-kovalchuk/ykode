package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

func TestCommandsReachTheirHandlers(t *testing.T) {
	var out, log bytes.Buffer
	r := NewREPL(agent.New(nil, nil, "m", agent.Options{}, nil), NewLineSource(strings.NewReader("")), &out, &log)
	for _, line := range []string{"/help", "/usage", "/reset", "/sideways"} {
		if quit, err := r.command(context.Background(), make(chan os.Signal), line); quit || err != nil {
			t.Fatalf("%s: quit=%v err=%v", line, quit, err)
		}
	}
	printed := out.String() + log.String()
	for _, want := range []string{"commands:", "usage:", "history cleared", "unknown command"} {
		if !strings.Contains(printed, want) {
			t.Errorf("%q was never printed:\n%s", want, printed)
		}
	}
}

// fakeUndo records that /undo reached the stack; tools.UndoStack does the real work.
type fakeUndo struct{ undone int }

func (f *fakeUndo) Undo(bool) (string, error) { f.undone++; return "put back src/a.go", nil }
func (f *fakeUndo) Empty() bool               { return f.undone > 0 }

func TestUndoPutsTheLastChangeBack(t *testing.T) {
	var out, log bytes.Buffer
	undo := &fakeUndo{}
	r := NewREPL(agent.New(nil, nil, "m", agent.Options{Undo: undo}, nil), NewLineSource(strings.NewReader("")), &out, &log)

	if quit, err := r.command(context.Background(), make(chan os.Signal), "/undo"); quit || err != nil {
		t.Fatalf("quit=%v err=%v", quit, err)
	}
	if undo.undone != 1 {
		t.Errorf("/undo reached the stack %d times", undo.undone)
	}
	if !strings.Contains(out.String(), "put back src/a.go") {
		t.Errorf("the undo said nothing useful: %q", out.String())
	}

	if quit, err := r.command(context.Background(), make(chan os.Signal), "/undo"); quit || err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing to undo") {
		t.Errorf("an empty stack should say so: %q", out.String())
	}
}
