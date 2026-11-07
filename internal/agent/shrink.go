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

// readsTheFile are the tools whose result is a copy of something on disk. Storing it would let
// recall hand back text a later edit already changed, so a shortened read gets a pointer instead.
var readsTheFile = map[string]bool{"read_file": true}

// summarize is what ykode keeps of a dropped result: which call it was, how big it was, how it began, and where the text is.
func (a *Agent) summarize(m *llm.Message, id string) string {
	name := m.Name
	if name == "" {
		name = "tool"
	}
	text := fmt.Sprintf("%s %s output (it was %d bytes) to fit the context; it began: %q",
		shrinkMark, name, len(m.Content), oneLine(m.Content, 120))
	if readsTheFile[name] {
		return text + " the file on disk has not changed unless you edited it: read the range you need again]"
	}
	if !a.opts.Store.Saved() {
		return text + "]"
	}
	if err := a.opts.Store.Save(id, m.Content); err != nil {
		a.tell(fmt.Sprintf("could not keep the full %s output: %v", name, err))
		return text + "]"
	}
	return fmt.Sprintf("%s: recall {\"id\":\"%s\"} for the whole text, or with pattern for just the lines you need]", text, id)
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
		Path    string `json:"path"`
		Content string `json:"content"`
		Edits   []struct {
			New string `json:"new"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(args, &payload); err != nil || payload.Path == "" {
		return nil, false
	}
	line := fmt.Sprintf("%s wrote %s to %s to fit the context; the file on disk has it", shrinkMark,
		byteSize(len(payload.Content)), payload.Path)
	if name == "edit_file" {
		written := 0
		for _, e := range payload.Edits {
			written += len(e.New)
		}
		line = fmt.Sprintf("%s changed %s: %d replacement(s), %s of new text, to fit the context; the file on disk has it",
			shrinkMark, payload.Path, len(payload.Edits), byteSize(written))
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
