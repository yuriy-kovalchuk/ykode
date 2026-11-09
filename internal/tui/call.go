package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// What one call leaves on the screen: a row's words, and at most one body under it. The rest of what
// a call returned stays where it belongs — the file it changed, the log a command wrote, the context
// the model is reading. Nothing is held for a screen that is not going to draw it.
const (
	changeKept   = 60   // lines of a change kept, which is more than the rows ever show
	tailKept     = 1200 // bytes of a failure's last lines
	tailShown    = 8    // rows of a failure the screen takes
	execNoteRun  = 100  // how much of a command fits in its row
	execNoteTail = 40   // and how much of what it ended with
	tailLead     = "    │ "
)

// call is what the screen remembers of one call.
type call struct {
	note   string // one line: what the call touched, and what came back
	change string // a change's hunks, capped; empty when the call changed nothing
	left   int    // lines of the change this text left out
	tail   string // a failure's last lines, capped; empty when nothing failed
}

// readCall is what a finished call leaves behind: its sentence, then either the change it made or,
// when it went wrong, the lines that say why.
func readCall(tool, args, result string, failed bool, root string) call {
	c := call{note: noteFor(tool, args, result, failed, root)}
	if holdsDiff(result) {
		_, body, _ := strings.Cut(result, "\n")
		c.change, c.left = capChange(body)
	} else if failed || exitSaid(result) != "" {
		c.tail = lastLines(withoutItsNote(result), tailKept)
	}
	return c
}

// noteFor says a call in one line. While it runs, that is what it was asked for. Once it answers, the
// tool's own sentence carries the row — it already names the file it touched — and what the call was
// asked for is added only when that sentence forgot to.
func noteFor(tool, args, result string, failed bool, root string) string {
	what := described(tool, args, root)
	if result == "" {
		return what
	}
	said := shorten(summaryLine(result), root)
	switch {
	case failed:
		return oneLine(what+" · "+said, noticeChars)
	case tool == "exec_command" && what != "":
		how := exitSaid(result)
		if how == "" {
			how = printedSaid(result)
		}
		return oneLine(what+" · "+oneLine(how, execNoteTail), noticeChars)
	case tool == "plan" && what != "":
		return what // how far the plan got and what it is on now: the pinned panel has the steps
	case tool == "search_text" || tool == "find_files":
		// what a locate found is counted, not printed
		return what + " · " + countSaid(result, foundWord(tool))
	case said == "":
		return what
	case tool == "read_file" || tool == "edit_file" || tool == "write_file":
		// the tool's own sentence names the file it touched; when it did not, the row says it
		if file := namedFile(args, root); file != "" && !strings.Contains(said, file) {
			return what + " · " + said
		}
		return said
	default:
		return said
	}
}

// described puts a call's arguments in words, and says nothing when they cannot be read: the row is
// better quiet than a strip of JSON.
func described(tool, args, root string) string {
	if args == "" || !json.Valid([]byte(args)) {
		return ""
	}
	return shorten(oneLine(tools.Describe(tool, args), execNoteRun), root)
}

// namedFile is the file a call names, written from where the project starts.
func namedFile(args, root string) string {
	var in struct {
		Path string `json:"path"`
	}
	if args == "" || json.Unmarshal([]byte(args), &in) != nil {
		return ""
	}
	return shorten(in.Path, root)
}

// foundWord is what a locate counts: files it named, or lines where a pattern turned up.
func foundWord(tool string) string {
	if tool == "find_files" {
		return "files"
	}
	return "matches"
}

// countSaid counts what a locate returned. Its result is one line per thing it found, so the row can
// say how much came back without putting any of it on the screen.
func countSaid(result, word string) string {
	if strings.HasPrefix(result, "(no matches") {
		return "no matches"
	}
	n := len(strings.Split(strings.TrimRight(result, "\n"), "\n"))
	at := ""
	if strings.Contains(result, "[stopped at the ") {
		at = " · its limit"
	}
	return fmt.Sprintf("%d %s%s", n, word, at)
}

// exitSaid is how a command ended, read from the note the tool leaves at the end of its output. An
// empty string means it simply ran and printed.
func exitSaid(result string) string {
	for _, line := range strings.Split(result, "\n") {
		switch {
		case strings.HasPrefix(line, "[exit status "):
			return "exit " + oneLine(strings.TrimSuffix(strings.TrimPrefix(line, "[exit status "), "]"), 12)
		case strings.HasPrefix(line, "[command killed after its "):
			return "killed at its timeout"
		case line == "[cancelled]":
			return "cancelled"
		}
	}
	return ""
}

// printedSaid is what a command that ran printed, when it printed anything.
func printedSaid(result string) string {
	switch {
	case strings.HasPrefix(result, "exit 0, no output"):
		return "no output"
	case strings.TrimSpace(result) == "":
		return "no output"
	}
	return fmt.Sprintf("%d lines", len(strings.Split(strings.TrimRight(result, "\n"), "\n")))
}

// withoutItsNote drops the line the tool adds about how the command left: the row already says it, and
// repeating it under the row is noise where the explanation should be.
func withoutItsNote(result string) string {
	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	for len(lines) > 0 {
		last := lines[len(lines)-1]
		if !strings.HasPrefix(last, "[") {
			break
		}
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// capChange keeps the head of a change and counts what fell off, so a hundred line write costs the
// run a hundred lines rather than a hundred lines times every redraw.
func capChange(body string) (string, int) {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) <= changeKept {
		return strings.Join(lines, "\n"), 0
	}
	return strings.Join(lines[:changeKept], "\n"), len(lines) - changeKept
}

// lastLines is the end of what a call answered: where an error is, and the part worth keeping. The
// cut starts at a line so no row begins in the middle of a word.
func lastLines(result string, budget int) string {
	if len(result) <= budget {
		return strings.TrimRight(result, "\n")
	}
	cut := len(result) - budget
	if at := strings.Index(result[cut:], "\n"); at >= 0 {
		cut += at + 1
	}
	return strings.TrimRight(result[cut:], "\n")
}

// shorten writes a path from where the project starts, and a path outside it from home.
func shorten(s, root string) string {
	if root != "" {
		s = strings.ReplaceAll(s, strings.TrimSuffix(root, "/")+"/", "")
	}
	return shortenHome(s)
}

// tailRows is what went wrong, as the rows under the row that did it: quiet, behind a gutter, with
// no ground of their own. A line the row above already said is not said again.
func tailRows(tail, note string, width int) []string {
	if tail == "" || strings.TrimSpace(tail) == "" {
		return nil
	}
	lines := strings.Split(tail, "\n")
	if len(lines) == 1 && strings.Contains(note, strings.TrimSpace(lines[0])) {
		return nil
	}
	left := 0
	if len(lines) > tailShown {
		left = len(lines) - tailShown
		lines = lines[len(lines)-tailShown:]
	}
	rows := make([]string, 0, len(lines)+1)
	if left > 0 {
		rows = append(rows, styleDim.Render(tailLead+cutToWidth(fmt.Sprintf("… %d earlier lines", left), width-len(tailLead))))
	}
	for _, line := range lines {
		rows = append(rows, styleDim.Render(tailLead+cutToWidth(diffText(line), width-len(tailLead)-1)))
	}
	return rows
}

// heldCall is what a finished call still costs the run, in bytes.
func heldCall(c call) int { return len(c.note) + len(c.change) + len(c.tail) }
