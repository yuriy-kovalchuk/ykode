package agent

import (
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// classOf answers what a name written in the conversation does to the project. A name the run no
// longer offers - a tool switched off, a conversation resumed from an older build - reads as the
// safe one, which is the zero value.
func (a *Agent) classOf(name string) tools.Class {
	if t, ok := a.tools.Get(name); ok {
		return t.Class()
	}
	return tools.ClassRuns
}

// changesProject says a tool may have changed what is on disk, so what was read before is no longer
// the answer.
func (a *Agent) changesProject(name string) bool {
	switch a.classOf(name) {
	case tools.ClassRuns, tools.ClassWrites:
		return true
	}
	return false
}

// runsTogether says several calls of this tool may overlap. Anything else waits its turn and keeps
// the order the model asked for.
func (a *Agent) runsTogether(name string) bool { return a.classOf(name) == tools.ClassReads }

// worthGuarding is a call the model can be pointed at again instead of running: a read with no
// memory of its own. read_file remembers what it has already shown by content, so it is left alone.
func worthGuarding(t tools.Tool) bool {
	if t.Class() != tools.ClassReads {
		return false
	}
	_, remembers := t.(tools.Forgetter)
	return !remembers
}
