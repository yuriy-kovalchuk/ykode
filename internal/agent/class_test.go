package agent

import (
	"os"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// every tool says what it does to the project, which decides overlap, reuse and dropped arguments
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

// a resumed conversation may name a tool this run dropped: it reads as a change, run alone
func TestAToolNobodyOffersReadsAsAChange(t *testing.T) {
	a := New(nil, tools.NewRegistry(), "m", Options{}, &captureSink{})
	if !a.changesProject("old_tool") {
		t.Error("an unknown tool was not taken for a change to the project")
	}
	if a.runsTogether("old_tool") {
		t.Error("an unknown tool was allowed to overlap another call")
	}
}
