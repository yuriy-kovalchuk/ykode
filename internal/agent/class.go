package agent

import (
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// classOf answers what a name in the conversation does to the project; an unknown name reads safe
func (a *Agent) classOf(name string) tools.Class {
	if t, ok := a.tools.Get(name); ok {
		return t.Class()
	}
	return tools.ClassRuns
}

// changesProject says a tool may have touched disk, so what was read before is no longer the answer
func (a *Agent) changesProject(name string) bool {
	switch a.classOf(name) {
	case tools.ClassRuns, tools.ClassWrites:
		return true
	}
	return false
}

// runsTogether says calls of this tool may overlap; anything else waits its turn in order
func (a *Agent) runsTogether(name string) bool { return a.classOf(name) == tools.ClassReads }
