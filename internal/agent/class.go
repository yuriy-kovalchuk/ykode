package agent

import (
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// A class says what a tool does to the project. That one answer decides the rest of how a call runs:
// whether calls may overlap, whether what was read before still holds the answer, whether a repeat
// can be answered from the result the model already has, and whether the arguments can be dropped
// once their text is on disk.
type class int

const (
	// A name missing from the table reads as this: serially, as a change, reusing nothing. The
	// reading that loses nothing, and the one the five separate maps used to fall back to.
	classRuns   class = iota // changes the project by doing something, not by writing out text
	classReads               // changes nothing, so several of them may run at once
	classWrites              // the argument is mostly text that ends up on disk
	classSays                // changes nothing and runs alone: the plan the screen reads, a question asked
)

// classes holds one entry per tool, so adding a tool is one line here rather than a round through
// five maps - which is how ask_user came to be taken for a change to the project.
var classes = map[string]class{
	"exec_command": classRuns,
	"find_files":   classReads,
	"search_text":  classReads,
	"read_file":    classReads,
	"write_file":   classWrites,
	"edit_file":    classWrites,
	"plan":         classSays,
	"ask_user":     classSays,
}

func classOf(name string) class { return classes[name] }

// changesProject says a tool may have changed what is on disk, so what was read before is no longer
// the answer.
func changesProject(name string) bool {
	return classOf(name) == classRuns || classOf(name) == classWrites
}

// runsTogether says several calls of this tool may overlap. Anything else waits its turn and keeps
// the order the model asked for.
func runsTogether(name string) bool { return classOf(name) == classReads }

// worthGuarding is a call the model can be pointed at again instead of running: a read with no
// memory of its own. read_file remembers what it has already shown by content, so it is left alone.
func worthGuarding(t tools.Tool) bool {
	if classOf(t.Name()) != classReads {
		return false
	}
	_, remembers := t.(tools.Forgetter)
	return !remembers
}
