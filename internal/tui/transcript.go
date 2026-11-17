package tui

import (
	"strings"
	"time"
)

// kind says who spoke.
type kind int

const (
	whoUser kind = iota
	whoThink
	whoAnswer
	whoTool
	whoNotice
	whoDraft   // a tool call the model is still writing
	whoDone    // where a prompt's work ended: when, and how long it took
	whoOutput  // what a slash command printed, line for line
	whoSummary // what the older turns were folded into, in the model's own lines
)

// entry is one block: a question, a run of thinking, an answer, one tool call, or a notice
type entry struct {
	who    kind
	text   string // user, thinking, answer, notice
	tool   string
	args   string // a call still being written, or one waiting for its answer
	call   call   // what a finished call leaves on the screen: see call.go
	asked  bool   // a call that has started and has not answered yet
	failed bool
	callID int // a draft: which call of the reply it is
	at     time.Time
	took   time.Duration

	state draftState // what has been read of a call the model is still writing; see draft.go

	// what a block still arriving keeps between draws, since it grows a token at a time
	arriving *strings.Builder // the text or the arguments, while more is on its way
	laid     layout           // an answer's blocks, laid out once each
	thought  flow             // thinking's paragraphs, wrapped once each
}

// write adds a piece to arriving text; the pieces gather in one buffer, so joining never copies
func (e *entry) write(sofar, piece string) string {
	if e.arriving == nil {
		e.arriving = &strings.Builder{}
		e.arriving.WriteString(sofar)
	}
	e.arriving.WriteString(piece)
	return e.arriving.String()
}

// transcript is what has been said; rows are kept and rebuilt only from the block that changed
type transcript struct {
	entries []entry
	stale   int    // the first entry that may need drawing again; len(entries) when none do
	width   int    // the screen the rows were drawn for
	root    string // the project the session runs in, so a path reads from there

	rowsCache []string // the rows the screen reads, one per row on the terminal
	rowStart  []int    // the row each entry already in rowsCache starts at
}

func (t *transcript) user(text string, at time.Time) {
	t.entries = append(t.entries, entry{who: whoUser, text: text, at: at})
}

// done closes a prompt's work with the time it ended and how long it took.
func (t *transcript) done(at time.Time, took time.Duration) {
	t.entries = append(t.entries, entry{who: whoDone, at: at, took: took})
}

// summary keeps the recap a compaction left behind, so what the session forgot is still readable.
func (t *transcript) summary(text string) {
	t.entries = append(t.entries, entry{who: whoSummary, text: text})
}

// output keeps a command's lines as it wrote them: /help is a table, not a sentence.
func (t *transcript) output(text string) {
	t.entries = append(t.entries, entry{who: whoOutput, text: text})
}

func (t *transcript) notice(text string) {
	t.entries = append(t.entries, entry{who: whoNotice, text: text})
}

// failed ends a call that did not run, landing the failure on the row already waiting
func (t *transcript) failed(name, args, why string) {
	c := call{note: oneLine(why, noticeChars), tail: lastLines(why, tailKept)}
	for i := len(t.entries) - 1; i >= 0; i-- {
		e := &t.entries[i]
		if e.who == whoTool && e.asked && e.tool == name && (args == "" || e.args == args) {
			if shown := noteFor(name, e.args, why, true, t.root); shown != "" {
				c.note = shown
			}
			e.call, e.failed, e.asked, e.args = c, true, false, ""
			t.mark(i)
			return
		}
	}
	t.entries = append(t.entries, entry{who: whoTool, tool: name, failed: true, call: c})
}

// thinking and answer stream in pieces: a piece joins the block that is still open.
func (t *transcript) thinking(text string) { t.open(whoThink, text) }
func (t *transcript) answer(text string)   { t.open(whoAnswer, text) }

func (t *transcript) open(who kind, text string) {
	if n := len(t.entries); n > 0 && t.entries[n-1].who == who {
		e := &t.entries[n-1]
		e.text = e.write(e.text, text)
		t.mark(n - 1)
		return
	}
	t.entries = append(t.entries, entry{who: who, text: text})
}

// mark says an entry changed since it was drawn, naming the earliest so drawing starts there
func (t *transcript) mark(at int) {
	if at < t.stale {
		t.stale = at
	}
}

// forget empties the transcript and its drawn rows when the run moves to another conversation
func (t *transcript) forget() {
	t.entries, t.stale = nil, 0
	t.rowsCache, t.rowStart = nil, nil
}

// draft adds a fragment of a call being written to that call's row.
func (t *transcript) draft(call int, tool, fragment string) {
	for i := len(t.entries) - 1; i >= 0 && t.entries[i].who == whoDraft; i-- {
		if e := &t.entries[i]; e.callID == call {
			e.args = e.write(e.args, fragment)
			if tool != "" {
				e.tool = tool // the name is whole by now, if its first piece was not
			}
			t.mark(i)
			return
		}
	}
	t.entries = append(t.entries, *newDraft(call, tool, fragment))
}

// newDraft is a call the model has begun writing: the numbers it reports start unset, not at zero.
func newDraft(call int, tool, args string) *entry {
	return &entry{who: whoDraft, tool: tool, args: args, callID: call, state: newDraftState()}
}

// dropDrafts removes calls being written when the reply ends, redrawing from the first removed
func (t *transcript) dropDrafts() {
	first := -1
	kept := t.entries[:0]
	for i, e := range t.entries {
		if e.who != whoDraft {
			kept = append(kept, e)
		} else if first < 0 {
			first = i
		}
	}
	if first < 0 {
		return
	}
	clear(t.entries[len(kept):]) // what moved up is not also held where it was
	t.entries = kept
	t.mark(first)
}

func (t *transcript) toolStart(name, args string) {
	// the row is worded when the call begins and when it answers, not once per redraw
	t.entries = append(t.entries, entry{who: whoTool, tool: name, args: args, asked: true,
		call: call{note: noteFor(name, args, "", false, t.root)}})
}

// toolEnd fills its call: parallel reads mean it may not be the newest row added
func (t *transcript) toolEnd(name, args, result string) { t.changed(name, args, result, "") }

// changed is toolEnd with the whole change: the model got the short copy, the screen the full one
func (t *transcript) changed(name, args, result, change string) {
	read := func() call {
		if change == "" {
			return readCall(name, args, result, false, t.root)
		}
		c := readCall(name, args, summaryLine(result), false, t.root)
		c.holds(change)
		return c
	}
	for i := len(t.entries) - 1; i >= 0; i-- {
		e := &t.entries[i]
		if e.who == whoTool && e.asked && e.tool == name && e.args == args {
			e.call = read()
			e.failed = exitSaid(result) != "" // a command that left non-zero failed, call or no call
			e.asked, e.args = false, ""       // what the call read is not what the screen keeps
			t.mark(i)
			return
		}
	}
	t.entries = append(t.entries, entry{who: whoTool, tool: name, failed: exitSaid(result) != "", call: read()})
}
