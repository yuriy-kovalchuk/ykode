package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

const (
	summaryFloor = 400 // tool results smaller than this are left alone
	keepResults  = 2   // the two newest results stay word for word
	shrinkMark   = "[ykode shortened this"
)

// underPressure says the conversation passed 3/4 of the server window, where context starts being worth money.
func (a *Agent) underPressure() bool {
	return a.opts.CtxSize > 0 && a.estimateTokens() > a.opts.CtxSize*3/4
}

// shrinkWhatAged shortens old results and the arguments of old file writes together: both are
// text the model has already been paid for.
func (a *Agent) shrinkWhatAged(keep int) int {
	return a.shrinkOldResults(keep) + a.shrinkOldEdits(keep)
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
		m := &a.history[i]
		if m.CallID == "" || len(m.Content) <= summaryFloor || strings.HasPrefix(m.Content, shrinkMark) {
			continue
		}
		summary := a.summarize(m, m.CallID)
		a.forget(*m)
		freed += len(m.Content) - len(summary)
		m.Content = summary
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
		shrinkMark, name, len(m.Content), oneLine(m.Content, 120))
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

// writesTheFile are the tools whose argument is mostly text the tool has already put on disk.
var writesTheFile = map[string]bool{"write_file": true, "edit_file": true}

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
			if !writesTheFile[c.Function.Name] {
				continue
			}
			reply := -1
			if j < len(replies) {
				reply = replies[j]
			}
			if reply < 0 || strings.HasPrefix(a.history[reply].Content, "error:") {
				continue // a call that failed keeps its text: a retry may need it
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
	}
	return freed
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
	line := fmt.Sprintf("%s wrote %s to %s to fit the context; the file on disk has it", shrinkMark,
		byteSize(len(payload.Content)), payload.Path)
	if name == "edit_file" {
		written, count := len(payload.NewText)+len(payload.NewString), len(payload.Edits)
		for _, e := range payload.Edits {
			written += len(e.NewText) + len(e.NewString)
		}
		if count == 0 {
			count = 1 // the one-change form: old_text and new_text at the top
		}
		line = fmt.Sprintf("%s changed %s: %d replacement(s), %s of new text, to fit the context; the file on disk has it",
			shrinkMark, payload.Path, count, byteSize(written))
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

// byteSize reads like a file listing: 640 B, 2.1 KB.
func byteSize(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1024)
}
