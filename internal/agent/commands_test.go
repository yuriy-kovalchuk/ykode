package agent

import (
	"strings"
	"testing"
)

func TestEveryCommandAnswersSomething(t *testing.T) {
	a := New(nil, nil, "m", Options{}, nil)
	for _, line := range []string{"/help", "/usage", "/reset", "/sideways"} {
		text, quit, err := a.Command(line)
		if quit || err != nil {
			t.Fatalf("%s: quit=%v err=%v", line, quit, err)
		}
		if text == "" {
			t.Errorf("%s answered nothing", line)
		}
	}
}

func TestHelpListsEveryCommand(t *testing.T) {
	for _, want := range []string{"/help", "/usage", "/reset", "/exit", "/undo", "/history", "/compact"} {
		if !strings.Contains(Help, want) {
			t.Errorf("help is missing %q:\n%s", want, Help)
		}
	}
}

func TestExitStopsTheSession(t *testing.T) {
	a := New(nil, nil, "m", Options{}, nil)
	if _, quit, _ := a.Command("/exit"); !quit {
		t.Error("/exit did not end the session")
	}
	if _, quit, _ := a.Command("/help"); quit {
		t.Error("/help ended the session")
	}
}

// countingUndo stands in for tools.UndoStack, which its own tests cover.
type countingUndo struct{ undone int }

func (c *countingUndo) Undo() (string, error) { c.undone++; return "put back src/a.go", nil }
func (c *countingUndo) Empty() bool           { return c.undone > 0 }

func TestUndoTravelsWithTheAgent(t *testing.T) {
	undo := &countingUndo{}
	a := New(nil, nil, "m", Options{Undo: undo}, nil)

	text, _, err := a.Command("/undo")
	if err != nil {
		t.Fatal(err)
	}
	if undo.undone != 1 {
		t.Errorf("/undo reached the stack %d times", undo.undone)
	}
	if !strings.Contains(text, "put back src/a.go") {
		t.Errorf("the undo said nothing useful: %q", text)
	}

	text, _, err = a.Command("/undo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "nothing to undo") {
		t.Errorf("an empty stack should say so: %q", text)
	}
}
