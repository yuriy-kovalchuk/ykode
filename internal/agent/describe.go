package agent

import (
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// describe is what a confirmation asks the user about: the call in the words the tools use, and
// the paths that fall outside the project when any do.
func describe(name, argsText string, outside []string) string {
	what := tools.Describe(name, argsText)
	if len(outside) == 0 {
		return name + " " + what
	}
	return fmt.Sprintf("%s OUTSIDE the project root: %s\n  %s", name, strings.Join(outside, ", "), what)
}

// oneLine squashes s onto one short line.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "..."
	}
	return s
}

// detailOf asks a tool what its last result says beyond the text.
func detailOf(tool tools.Tool) tools.Detail {
	if tool.Class() == tools.ClassReads {
		return tools.Detail{} // calls that overlap have no last result of their own to report
	}
	if d, ok := tool.(tools.Detailed); ok {
		return d.LastDetail()
	}
	return tools.Detail{}
}
