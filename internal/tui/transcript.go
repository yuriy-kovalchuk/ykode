package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// kind says who spoke.
type kind int

const (
	whoUser kind = iota
	whoThink
	whoAnswer
	whoTool
	whoNotice
	whoDraft  // a tool call the model is still writing
	whoDone   // where a prompt's work ended: when, and how long it took
	whoOutput // what a slash command printed, line for line
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
	call   int // a draft: which call of the reply it is
	at     time.Time
	took   time.Duration
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

func (t *transcript) user(text string, at time.Time) {
	t.entries = append(t.entries, entry{who: whoUser, text: text, at: at})
}

// done closes a prompt's work with the time it ended and how long it took.
func (t *transcript) done(at time.Time, took time.Duration) {
	t.entries = append(t.entries, entry{who: whoDone, at: at, took: took})
}

// output keeps a command's lines as it wrote them: /help is a table, not a sentence.
func (t *transcript) output(text string) {
	t.entries = append(t.entries, entry{who: whoOutput, text: text})
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

// draft adds a fragment of a call being written to that call's row.
func (t *transcript) draft(call int, tool, fragment string) {
	for i := len(t.entries) - 1; i >= 0 && t.entries[i].who == whoDraft; i-- {
		if t.entries[i].call == call {
			t.entries[i].args += fragment
			return
		}
	}
	t.entries = append(t.entries, entry{who: whoDraft, tool: tool, args: fragment, call: call})
}

// dropDrafts removes the calls being written once the reply is done: the tool rows take their place.
func (t *transcript) dropDrafts() {
	kept := t.entries[:0]
	for _, e := range t.entries {
		if e.who != whoDraft {
			kept = append(kept, e)
		}
	}
	t.entries = kept
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
			fmt.Fprintf(&b, "%s\n\n", rightAligned(styleUserBar.Render("▌")+" "+styleBold.Render(e.text), styleDim.Render(clock(e.at)), width))
		case whoOutput:
			fmt.Fprintf(&b, "%s\n\n", indent(e.text, "  "))
		case whoDone:
			fmt.Fprintf(&b, "%s\n\n", styleDim.Render(fmt.Sprintf("  %s · %s", clock(e.at), duration(e.took))))
		case whoThink:
			fmt.Fprintf(&b, "%s\n\n", gutter(wrap(e.text, width-4), styleThink))
		case whoAnswer:
			fmt.Fprintf(&b, "%s\n\n", e.text)
		case whoNotice:
			fmt.Fprintf(&b, "%s\n", styleDim.Render("· "+oneLine(e.text, noticeChars)))
		case whoTool:
			b.WriteString(toolRow(e, width) + "\n")
			if expanded && e.args != "" {
				fmt.Fprintf(&b, "%s\n", indent(wrap(e.args, width-2), "  "))
			}
		case whoDraft:
			b.WriteString(draftRows(draft{call: e.call, tool: e.tool, args: e.args}, width) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// toolRow is one call on one row: the arguments give way to the result.
func toolRow(e entry, width int) string {
	mark, tail := styleAccent.Render("→"), styleDim.Render("working…")
	switch {
	case e.failed:
		mark, tail = styleBad.Render("✗"), styleBad.Render(e.result)
	case e.result != "":
		mark, tail = styleOK.Render("✓"), e.result
	}
	return fmt.Sprintf("%s %s %s  %s", mark, styleToolName.Render(e.tool), styleDim.Render(oneLine(e.args, rowArgs(width, e.result))), tail)
}

// draftRows is a call being written: its head, then the newest lines of what it writes.
func draftRows(d draft, width int) string {
	head := fmt.Sprintf("%s %s %s", styleWorking.Render("✎"), styleToolName.Render(d.tool), styleDim.Render(d.summary()))
	lines, _, _ := d.preview()
	for i, line := range lines {
		lines[i] = cutToWidth(line, width-4)
	}
	if len(lines) == 0 {
		return head
	}
	return head + "\n" + gutter(strings.Join(lines, "\n"), styleDim)
}

// gutter marks a block that is not the answer, such as thinking or a call's preview.
func gutter(text string, style lipgloss.Style) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = styleDim.Render("  ┊ ") + style.Render(line)
	}
	return strings.Join(lines, "\n")
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

// clock is a message's time of day; the date is the session's, so it is left out.
func clock(at time.Time) string { return at.Format("15:04") }

// rightAligned puts tag at the end of the row when it fits, after the text when it does not.
func rightAligned(text, tag string, width int) string {
	gap := width - lipgloss.Width(text) - lipgloss.Width(tag)
	if gap < 2 {
		return text + "  " + tag
	}
	return text + strings.Repeat(" ", gap) + tag
}
