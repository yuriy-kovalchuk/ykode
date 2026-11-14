package agent

import (
	"os"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// every tool states what it does to the project: that one answer decides whether calls overlap,
// whether what was read before still holds the answer, and whether the arguments may be dropped
func TestEveryToolStatesItsClass(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	registry := tools.BuiltIn(root, dir, tools.Options{})
	registry.Add(tools.NewAskUser(nil)) // the agent adds it after the built-ins
	want := map[string]tools.Class{
		"find_files":   tools.ClassReads,
		"search_text":  tools.ClassReads,
		"read_file":    tools.ClassReads,
		"write_file":   tools.ClassWrites,
		"edit_file":    tools.ClassWrites,
		"exec_command": tools.ClassRuns,
		"plan":         tools.ClassSays,
		"ask_user":     tools.ClassSays,
	}
	for _, tool := range registry.All() {
		expected, known := want[tool.Name()]
		if !known {
			t.Errorf("%s is offered to the model and no test knows what it does to the project", tool.Name())
			continue
		}
		if got := tool.Class(); got != expected {
			t.Errorf("%s says it is class %d, wanted %d", tool.Name(), got, expected)
		}
		delete(want, tool.Name())
	}
	for name := range want {
		t.Errorf("%s is not in the registry the run builds", name)
	}
}

// a repeat of a read is answered from the result the model still has, unless the tool keeps its own
// memory of what it showed - which is how read_file notices an edited file
func TestOnlyReadsWithoutTheirOwnMemoryAreGuarded(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	for _, tool := range tools.BuiltIn(root, dir, tools.Options{}).All() {
		guarded, want := worthGuarding(tool), tool.Class() == tools.ClassReads && tool.Name() != "read_file"
		if guarded != want {
			t.Errorf("%s guarded = %v, wanted %v", tool.Name(), guarded, want)
		}
	}
}

// a resumed conversation can name a tool this run no longer offers: it reads as a change, run on its
// own, since nothing is known about it
func TestAToolNobodyOffersReadsAsAChange(t *testing.T) {
	a := New(nil, tools.NewRegistry(), "m", Options{}, &captureSink{})
	if !a.changesProject("old_tool") {
		t.Error("an unknown tool was not taken for a change to the project")
	}
	if a.runsTogether("old_tool") {
		t.Error("an unknown tool was allowed to overlap another call")
	}
}
