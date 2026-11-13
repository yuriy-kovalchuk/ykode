package agent

import (
	"os"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// a tool left out of the table runs serially and counts as a change to the project, quietly: the
// table must know every tool the run can be asked for
func TestEveryToolIsClassified(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	registry := tools.BuiltIn(root, dir, tools.Options{})
	want := map[string]class{
		"find_files":   classReads,
		"search_text":  classReads,
		"read_file":    classReads,
		"write_file":   classWrites,
		"edit_file":    classWrites,
		"exec_command": classRuns,
		"plan":         classSays,
		"ask_user":     classSays, // the agent adds it after the built-ins
	}
	for name, expected := range want {
		got, ok := classes[name]
		if !ok {
			t.Errorf("%s is not classified: it would run as an unknown tool", name)
			continue
		}
		if got != expected {
			t.Errorf("%s is class %d, wanted %d", name, got, expected)
		}
	}
	for _, name := range append(registry.Names(), "ask_user") {
		if _, ok := classes[name]; !ok {
			t.Errorf("%s is offered to the model but classified as nothing", name)
		}
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
	for _, tool := range tools.BuiltIn(root, dir, tools.Options{}).All() {
		guarded := worthGuarding(tool)
		if want := tool.Name() != "read_file" && runsTogether(tool.Name()); guarded != want {
			t.Errorf("%s guarded = %v, wanted %v", tool.Name(), guarded, want)
		}
	}
}
