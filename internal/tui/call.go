package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// what a call leaves on screen: a row's words and at most one body; the rest stays on disk
const (
	tailKept     = 1200 // bytes of a failure's last lines
	tailShown    = 8    // rows of a failure the screen takes
	execNoteRun  = 100  // how much of a command fits in its row
	execNoteTail = 40   // and how much of what it ended with
	tailLead     = "    │ "
)

// call is what the screen remembers of one call.
type call struct {
	note   string // one line: what the call touched, and what came back
	change string // a change's hunks, as the tool handed them over; empty when the call changed nothing
	tail   string // a failure's last lines, capped; empty when nothing failed
	full   string // a command as the model wrote it, for ctrl+r to show when the row cuts it
}

// readCall is a finished call's row, then the change it made or the lines saying why it failed
func readCall(tool, args, result string, failed bool, root string) call {
	c := call{note: noteFor(tool, args, result, failed, root), full: fullCommand(tool, args)}
	if holdsDiff(result) {
		_, body, _ := strings.Cut(result, "\n")
		c.holds(body)
	} else if failed || exitSaid(result) != "" {
		c.tail = lastLines(withoutItsNote(result), tailKept)
	}
	return c
}

// holds gives a call the change it made, and counts it on the call's row.
func (c *call) holds(change string) {
	c.change = strings.TrimRight(change, "\n")
	c.note = strings.TrimPrefix(c.note+" · "+changeSize(change), " · ")
}

// noteFor says a call in one line: the ask while it runs, the tool's sentence once it answers
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

// described puts a call's arguments in words, and stays quiet when they cannot be read
func described(tool, args, root string) string {
	if args == "" || !json.Valid([]byte(args)) {
		return ""
	}
	return shorten(oneLine(tools.Describe(tool, args), execNoteRun), root)
}

// fullCommand is what a command call ran, whole and with its own line breaks; "" for any other call
func fullCommand(tool, args string) string {
	var in struct {
		Command string `json:"command"`
	}
	if tool != "exec_command" || args == "" || json.Unmarshal([]byte(args), &in) != nil {
		return ""
	}
	return strings.TrimSpace(in.Command)
}

// fullRows is a command the row had to cut, whole under it; nil when the row already shows all of it
func fullRows(c call, root string, width int) []string {
	if c.full == "" {
		return nil
	}
	room := max(width-toolRowMark-toolNameWidth, toolRowArgs)
	if !strings.Contains(c.full, "\n") && strings.Contains(cutToWidth(oneLine(c.note, noticeChars), room), shorten(c.full, root)) {
		return nil
	}
	lead := lipgloss.Width(tailLead)
	var rows []string
	for _, line := range strings.Split(c.full, "\n") {
		for _, row := range wrapKept(line, max(width-lead, 1)) {
			rows = append(rows, styleDim.Render(tailLead+row))
		}
	}
	return rows
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

// exitSaid is how a command ended, from the note ending its output; "" means it simply ran
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

// withoutItsNote drops the tool's exit line from the body, since the row already says it
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

// lastLines is the end of an answer, where an error is, cut at a line so no row starts mid-word
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

// tailRows is what went wrong under the row that did it, without repeating the row above
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
	room := width - lipgloss.Width(tailLead) // the gutter's bar is three bytes and one column
	if left > 0 {
		rows = append(rows, styleDim.Render(tailLead+cutToWidth(fmt.Sprintf("… %d earlier lines", left), room)))
	}
	for _, line := range lines {
		rows = append(rows, styleDim.Render(tailLead+cutToWidth(diffText(line), room-1)))
	}
	return rows
}

// heldCall is what a finished call still costs the run, in bytes.
func heldCall(c call) int { return len(c.note) + len(c.change) + len(c.tail) + len(c.full) }
