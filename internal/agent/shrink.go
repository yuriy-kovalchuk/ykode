package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/size"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

const (
	summaryFloor = 400 // tool results smaller than this are left alone
	keepResults  = 2   // the two newest results stay word for word
	ShrinkMark   = "[ykode shortened this"
)

// underPressure says the conversation passed the mark where context starts being worth money.
func (a *Agent) underPressure() bool {
	return a.opts.CtxSize > 0 && a.estimateTokens() > a.pressureMark()
}

// shrinkWhatAged shortens old results and the arguments of old file writes together: both are
// text the model has already been paid for. It also says how much of what went was thinking, which is
// worth naming: it is not output the model asked for.
func (a *Agent) shrinkWhatAged(keep int) (freed, thinking int) {
	thinking = a.shrinkOldReasoning(keep)
	freed = thinking + a.shrinkOldResults(keep) + a.shrinkOldEdits(keep)
	if freed-thinking > 0 { // matches shrinkNotice: thinking alone is not a loss the footer reports
		a.work.made(roomShortened)
	}
	return freed, thinking
}

// shrinkNotice is what the shortening is worth interrupting the user for: text the model loses is worth a
// line. Thinking that stops being re-sent is per-turn housekeeping the user cannot act on, and it repeats
// on every resume of a long conversation, so it is not announced.
func shrinkNotice(freed, thinking int) string {
	if real := freed - thinking; real > 0 {
		return fmt.Sprintf("shortened older tool output to fit the context (%s)", size.Text(real))
	}
	return ""
}

// shrinkOldReasoning stops sending the thinking of all but the newest replies. Sent back, the thinking
// keeps the model's own line of reasoning in front of it across a tool loop; the oldest of it is also
// the bulk of a long run, and the first thing that can go when room runs short.
func (a *Agent) shrinkOldReasoning(keep int) int {
	freed, seen := 0, 0
	for i := len(a.history) - 1; i >= 0; i-- {
		m := &a.history[i]
		if m.Role != "assistant" || m.WireReasoning == "" {
			continue
		}
		if seen++; seen > keep {
			freed += len(m.WireReasoning)
			m.WireReasoning = ""
			a.revised(i) // the log keeps it as stopped: a run that comes back must not free it again
		}
	}
	return freed
}

// shrinkOldResults replaces older large tool results with a summary the code writes, and returns the bytes freed.
func (a *Agent) shrinkOldResults(keep int) int {
	var positions []int
	for i, m := range a.history {
		if m.Role == "tool" {
			positions = append(positions, i)
		}
	}
	freed := 0
	for n, i := range positions {
		if n >= len(positions)-keep {
			break // the model is still working with these
		}
		if i >= a.seenThrough {
			continue // this result has not reached a model: shortening it loses text nobody has read
		}
		m := &a.history[i]
		if m.CallID == "" || len(m.Content) <= summaryFloor || strings.HasPrefix(m.Content, ShrinkMark) {
			continue
		}
		summary := a.summarize(m, m.CallID)
		a.forget(*m)
		freed += len(m.Content) - len(summary)
		m.Content = summary
		a.revised(i)
	}
	return freed
}

// askAgain: asking again is cheaper than a stored copy, which a later edit would make stale.
var askAgain = map[string]string{
	"read_file":   "the file on disk has not changed unless you edited it: read the range you need again",
	"find_files":  "run the same find_files again if you need it",
	"search_text": "run the same search_text again if you need it",
	"write_file":  "the file on disk has the result: read it if you need it",
	"edit_file":   "the file on disk has the result: read it if you need it",
}

// summarize keeps only command output on disk: running a test suite again costs minutes.
func (a *Agent) summarize(m *llm.Message, id string) string {
	name := m.Name
	if name == "" {
		name = "tool"
	}
	text := fmt.Sprintf("%s %s output (it was %d bytes) to fit the context; it began: %q",
		ShrinkMark, name, len(m.Content), oneLine(m.Content, 120))
	if how, ok := askAgain[name]; ok {
		return text + " " + how + "]"
	}
	if !a.opts.Store.Saved() {
		return text + " run it again if you need the rest]"
	}
	path, err := a.opts.Store.Save(id, m.Content)
	if err != nil {
		a.tell(fmt.Sprintf("could not keep the full %s output: %v", name, err))
		return text + " run it again if you need the rest]"
	}
	return fmt.Sprintf("%s the whole text is in %s: grep it or read part of it with exec_command, e.g. sed -n '1,80p' %s]", text, path, path)
}

// argMark keys the one-line replacement, so shrinking twice changes nothing.
const argMark = `"_ykode"`

// shrinkOldEdits replaces the arguments of older write_file and edit_file calls with one line
// saying what they did. The text is on disk; resending it every turn pays for a file the model
// has already written. A call that failed keeps its arguments, since retrying it may need them.
func (a *Agent) shrinkOldEdits(keep int) int {
	type site struct{ msg, call, reply int }
	var sites []site
	for i, m := range a.history {
		replies := a.repliesFor(i)
		for j, c := range m.ToolCalls {
			if a.classOf(c.Function.Name) != tools.ClassWrites {
				continue
			}
			reply := -1
			if j < len(replies) {
				reply = replies[j]
			}
			if reply < 0 || !ranToTheEnd(a.history[reply].Content) {
				continue
			}
			sites = append(sites, site{i, j, reply})
		}
	}
	freed := 0
	for n := 0; n < len(sites)-keep; n++ {
		s := sites[n]
		c := &a.history[s.msg].ToolCalls[s.call]
		args := bytes.TrimSpace(c.Function.Arguments)
		if len(args) <= summaryFloor || strings.Contains(string(args), argMark) {
			continue
		}
		summary, ok := editSummary(c.Function.Name, c.Function, a.history[s.reply].CallID)
		if !ok {
			continue
		}
		freed += len(args) - len(summary)
		c.Function.Arguments = summary
		a.revised(s.msg)
	}
	return freed
}

// ranToTheEnd says a write's reply is that of a write that happened. One that failed, or was
// interrupted before it ran, keeps its arguments: a retry needs them, and "the file on disk has it"
// said of a write that never happened is a lie the model would build on.
func ranToTheEnd(reply string) bool {
	return !strings.HasPrefix(reply, "error:") && reply != neverRan
}

// editSummary says what a file write did without carrying the text it wrote.
func editSummary(name string, call llm.FunctionCall, id string) (json.RawMessage, bool) {
	args, err := call.ArgsJSON()
	if err != nil {
		return nil, false
	}
	var payload struct {
		Path      string `json:"path"`
		Content   string `json:"content"`
		NewText   string `json:"new_text"`   // the one-change form
		NewString string `json:"new_string"` // the name models borrow from other tools
		Edits     []struct {
			NewText   string `json:"new_text"`
			NewString string `json:"new_string"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(args, &payload); err != nil || payload.Path == "" {
		return nil, false
	}
	line := fmt.Sprintf("%s wrote %s to %s to fit the context; the file on disk has it", ShrinkMark,
		size.Text(len(payload.Content)), payload.Path)
	if name == "edit_file" {
		written, count := len(payload.NewText)+len(payload.NewString), len(payload.Edits)
		for _, e := range payload.Edits {
			written += len(e.NewText) + len(e.NewString)
		}
		if count == 0 {
			count = 1 // the one-change form: old_text and new_text at the top
		}
		line = fmt.Sprintf("%s changed %s: %d replacement(s), %s of new text, to fit the context; the file on disk has it",
			ShrinkMark, payload.Path, count, size.Text(written))
	}
	if id != "" {
		line += "; " + id + " was the reply"
	}
	raw, err := json.Marshal(map[string]string{"_ykode": line})
	if err != nil {
		return nil, false
	}
	return raw, true
}

// repliesFor lists the tool messages that answer one assistant message, in call order. The
// server's own ids repeat between runs, so position is what pairs a call with its reply.
func (a *Agent) repliesFor(assistant int) []int {
	var replies []int
	for i := assistant + 1; i < len(a.history) && len(replies) < len(a.history[assistant].ToolCalls); i++ {
		if a.history[i].Role == "tool" {
			replies = append(replies, i)
		}
	}
	return replies
}
