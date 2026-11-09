package tui

import (
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

// entry is one block of the transcript: a question, a run of thinking, an answer,
// one tool call, or something the run did without being asked.
type entry struct {
	who    kind
	text   string // user, thinking, answer, notice
	tool   string
	args   string // a call still being written, or one waiting for its answer
	call   call   // what a finished call leaves on the screen: see call.go
	failed bool
	callID int // a draft: which call of the reply it is
	at     time.Time
	took   time.Duration

	state draftState // what has been read of a call the model is still writing; see draft.go

	blocks highlightCache // the coloured code blocks of this entry, so redrawing it colours each once
}

// transcript is what has been said so far. Rendering is separate so the rules can be tested without
// a screen. The rows it drew are kept between draws and only rebuilt from the block that changed:
// a streamed token redraws the screen while only the block still open has new text in it, and a
// session of eight thousand rows must not be drawn again to add one.
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

// failed ends a call that did not run or did not finish. The call's own row is waiting for an answer,
// so the failure lands on it: a row left asking forever beside a second one that says why is two
// rows for one call.
func (t *transcript) failed(name, args, why string) {
	c := call{note: oneLine(why, noticeChars), tail: lastLines(why, tailKept)}
	for i := len(t.entries) - 1; i >= 0; i-- {
		e := &t.entries[i]
		if e.who == whoTool && e.args != "" && e.tool == name && (args == "" || e.args == args) {
			if shown := noteFor(name, e.args, why, true, t.root); shown != "" {
				c.note = shown
			}
			e.call, e.failed, e.args = c, true, ""
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
		t.entries[n-1].text += text
		t.mark(n - 1)
		return
	}
	t.entries = append(t.entries, entry{who: who, text: text})
}

// mark says an entry has changed since it was drawn. Entries are only ever added at the end or
// changed in place, so naming the earliest one changed covers the rest: drawing starts there.
func (t *transcript) mark(at int) {
	if at < t.stale {
		t.stale = at
	}
}

// draft adds a fragment of a call being written to that call's row.
func (t *transcript) draft(call int, tool, fragment string) {
	for i := len(t.entries) - 1; i >= 0 && t.entries[i].who == whoDraft; i-- {
		if t.entries[i].callID == call {
			t.entries[i].args += fragment
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

// dropDrafts removes the calls being written once the reply is done: the tool rows take their
// place, and every row after the first one removed moves up, so nothing keeps its drawing.
func (t *transcript) dropDrafts() {
	kept := t.entries[:0]
	for _, e := range t.entries {
		if e.who != whoDraft {
			kept = append(kept, e)
		}
	}
	t.entries, t.stale = kept, 0
	t.rowsCache, t.rowStart = nil, nil
}

func (t *transcript) toolStart(name, args string) {
	// the row is worded once, when the call begins, and again when it answers: a call being drawn a
	// hundred times a second is not parsed a hundred times a second
	t.entries = append(t.entries, entry{who: whoTool, tool: name, args: args,
		call: call{note: noteFor(name, args, "", false, t.root)}})
}

// toolEnd fills the call it belongs to. Parallel reads mean the reply may not be the last
// row added, so it finds the newest unfinished call with the same tool and arguments.
func (t *transcript) toolEnd(name, args, result string) {
	for i := len(t.entries) - 1; i >= 0; i-- {
		e := &t.entries[i]
		if e.who == whoTool && e.args != "" && e.args == args {
			e.call = readCall(name, args, result, false, t.root)
			e.failed = exitSaid(result) != "" // a command that left non-zero failed, call or no call
			e.args = ""                       // what the call read is not what the screen keeps
			t.mark(i)
			return
		}
	}
	c := readCall(name, args, result, false, t.root)
	t.entries = append(t.entries, entry{who: whoTool, tool: name, failed: exitSaid(result) != "", call: c})
}
