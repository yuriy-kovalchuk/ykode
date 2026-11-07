package tui

import (
	"fmt"
	"strings"
)

// kind says who spoke.
type kind int

const (
	whoUser kind = iota
	whoThink
	whoAnswer
	whoTool
	whoNotice
)

// entry is one block of the transcript: a question, a run of thinking, an answer,
// one tool call, or something the run did without being asked.
type entry struct {
	who    kind
	text   string // user, thinking, answer, notice
	tool   string
	args   string
	result string // what the tool answered, one line
	failed bool
}

// transcript is what has been said so far. Rendering is separate so the rules can be
// tested without a screen.
type transcript struct {
	entries []entry
}

const (
	noticeChars = 200
	toolRowTail = 20 // what is left for the result after the tool name on a row
)

func (t *transcript) user(text string) {
	t.entries = append(t.entries, entry{who: whoUser, text: text})
}
func (t *transcript) notice(text string) {
	t.entries = append(t.entries, entry{who: whoNotice, text: text})
}
func (t *transcript) failed(name, why string) {
	t.entries = append(t.entries, entry{who: whoTool, tool: name, result: why, failed: true})
}

// thinking and answer stream in pieces: a piece joins the block that is still open.
func (t *transcript) thinking(text string) { t.open(whoThink, text) }
func (t *transcript) answer(text string)   { t.open(whoAnswer, text) }

func (t *transcript) open(who kind, text string) {
	if n := len(t.entries); n > 0 && t.entries[n-1].who == who {
		t.entries[n-1].text += text
		return
	}
	t.entries = append(t.entries, entry{who: who, text: text})
}

func (t *transcript) toolStart(name, args string) {
	t.entries = append(t.entries, entry{who: whoTool, tool: name, args: args})
}

// toolEnd fills the call it belongs to. Parallel reads mean the reply may not be the last
// row added, so it finds the newest unfinished call with the same tool and arguments.
func (t *transcript) toolEnd(name, args, result string) {
	for i := len(t.entries) - 1; i >= 0; i-- {
		e := &t.entries[i]
		if e.who == whoTool && e.result == "" && e.tool == name && e.args == args {
			e.result = result
			return
		}
	}
	t.entries = append(t.entries, entry{who: whoTool, tool: name, result: result})
}

// render turns the transcript into lines for the viewport. A tool row is one line by
// default; expanded, it also shows what the call was asked to do.
func (t *transcript) render(width int, expanded bool) string {
	var b strings.Builder
	for _, e := range t.entries {
		switch e.who {
		case whoUser:
			fmt.Fprintf(&b, "\x1b[1m❯ %s\x1b[0m\n\n", e.text)
		case whoThink:
			fmt.Fprintf(&b, "\x1b[2;3m%s\x1b[0m\n\n", indent(wrap(e.text, width-2), "  "))
		case whoAnswer:
			fmt.Fprintf(&b, "%s\n\n", e.text)
		case whoNotice:
			fmt.Fprintf(&b, "\x1b[2;3m.. %s\x1b[0m\n", oneLine(e.text, noticeChars))
		case whoTool:
			mark, tail := "→", "\x1b[2mworking…\x1b[0m"
			if e.failed {
				mark = "✗"
			}
			if e.result != "" {
				tail = e.result
			}
			// one row per call: the arguments give way to the result, and open rows say more
			fmt.Fprintf(&b, "%s \x1b[2m%s\x1b[0m %s  %s\n", mark, e.tool, oneLine(e.args, rowArgs(width, tail)), tail)
			if expanded && e.args != "" {
				fmt.Fprintf(&b, "%s\n", indent(wrap(e.args, width-2), "  "))
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// rowArgs is what of the call fits on one row once the tool name and its result are placed.
func rowArgs(width int, result string) int {
	room := width - len(result) - len(oneLine("", 0)) - toolRowTail
	if room < 20 {
		room = 20
	}
	return room
}

func oneLine(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= max {
		return text
	}
	return text[:max] + "…"
}

// wrap breaks a line to width without cutting words, keeping long words intact.
func wrap(text string, width int) string {
	if width < 20 {
		width = 20
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			if line != "" && len(line)+1+len(word) > width {
				lines = append(lines, line)
				line = word
				continue
			}
			if line == "" {
				line = word
			} else {
				line += " " + word
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func indent(text, pad string) string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}
