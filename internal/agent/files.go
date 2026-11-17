package agent

import (
	"encoding/json"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

const (
	readFilesOpen     = "<read-files>"
	readFilesClose    = "</read-files>"
	modifiedFilesOpen = "<modified-files>"
	modifiedFilesEnd  = "</modified-files>"
	planOpen          = "<plan>"
	planClose         = "</plan>"
)

// noteFiles adds the folded turns' files to lists that accumulate across folds
func (a *Agent) noteFiles(folded []llm.Message) {
	for _, m := range folded {
		for _, call := range m.ToolCalls {
			path := pathOf(call.Function.Arguments)
			if path == "" {
				continue
			}
			switch call.Function.Name {
			case "read_file":
				a.readFiles = remember(a.readFiles, path)
			case "write_file", "edit_file":
				a.modifiedFiles = remember(a.modifiedFiles, path)
			}
		}
	}
}

func pathOf(args json.RawMessage) string {
	var in struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(args, &in) != nil {
		return ""
	}
	return strings.TrimSpace(in.Path)
}

// remember keeps a list of unique paths, the newest last, to the most a list names.
func remember(list []string, path string) []string {
	for i, p := range list {
		if p == path {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	list = append(list, path)
	if len(list) > maxListedFiles {
		list = list[len(list)-maxListedFiles:]
	}
	return list
}

// readOnlyFiles are the files read and never changed: a file that was changed is listed as modified.
func (a *Agent) readOnlyFiles() []string {
	changed := make(map[string]bool, len(a.modifiedFiles))
	for _, p := range a.modifiedFiles {
		changed[p] = true
	}
	var only []string
	for _, p := range a.readFiles {
		if !changed[p] {
			only = append(only, p)
		}
	}
	return only
}

// fileBlocks is the end of a recap: the files read and the files changed, in blocks to read back
func (a *Agent) fileBlocks() string {
	var b strings.Builder
	if read := a.readOnlyFiles(); len(read) > 0 {
		b.WriteString("\n\n" + readFilesOpen + "\n" + strings.Join(read, "\n") + "\n" + readFilesClose)
	}
	if len(a.modifiedFiles) > 0 {
		b.WriteString("\n\n" + modifiedFilesOpen + "\n" + strings.Join(a.modifiedFiles, "\n") + "\n" + modifiedFilesEnd)
	}
	return b.String()
}

// recapOf reads a fold's message back: the summary without the file blocks restoreFiles returns
func recapOf(m llm.Message) (string, bool) {
	text, ok := strings.CutPrefix(m.Content, FoldMark)
	if !ok || m.Role != "user" {
		return "", false
	}
	for _, open := range []string{readFilesOpen, modifiedFilesOpen, planOpen} {
		if at := strings.Index(text, "\n\n"+open); at >= 0 {
			text = text[:at] // the blocks after the summary are not part of it
		}
	}
	return strings.TrimSpace(text), true
}

// restoreFiles reads the file lists out of a resumed recap, so the next fold carries on from them
func (a *Agent) restoreFiles(history []llm.Message) {
	for _, m := range history {
		if _, ok := recapOf(m); !ok {
			continue
		}
		a.readFiles = block(m.Content, readFilesOpen, readFilesClose)
		a.modifiedFiles = block(m.Content, modifiedFilesOpen, modifiedFilesEnd)
		return
	}
}

func block(text, open, end string) []string {
	_, rest, ok := strings.Cut(text, open+"\n")
	if !ok {
		return nil
	}
	body, _, _ := strings.Cut(rest, "\n"+end)
	return strings.Split(body, "\n")
}

// planBlock is the plan at the end of a recap, so a fold keeps the list the model is working through
func (a *Agent) planBlock() string {
	if len(a.opts.Plan.Steps()) == 0 {
		return ""
	}
	return "\n\n" + planOpen + "\n" + a.opts.Plan.Report() + "\n" + planClose
}

// restorePlan finds the plan in a resumed conversation: the newest call that ran, or the recap's
func (a *Agent) restorePlan(history []llm.Message) {
	ran := map[string]bool{} // a call whose answer was an error left the plan as it was
	for _, m := range history {
		if m.Role == "tool" && m.Name == "plan" && !strings.HasPrefix(m.Content, "error:") {
			ran[m.ToolCallID] = true
		}
	}
	for i := len(history) - 1; i >= 0; i-- {
		for j := len(history[i].ToolCalls) - 1; j >= 0; j-- {
			call := history[i].ToolCalls[j]
			if call.Function.Name == "plan" && ran[call.ID] {
				a.opts.Plan.Restore(tools.StepsFrom(call.Function.Arguments))
				return
			}
		}
	}
	for _, m := range history {
		if _, folded := recapOf(m); folded {
			_, rest, _ := strings.Cut(m.Content, planOpen+"\n")
			body, _, _ := strings.Cut(rest, "\n"+planClose)
			a.opts.Plan.Restore(tools.StepsFromReport(body))
		}
	}
}
