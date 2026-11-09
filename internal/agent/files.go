package agent

import (
	"encoding/json"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// foldNote is what the fold in progress did, held until it is written to the session log.
type foldNote struct {
	summary string
	dropped int
	before  int
}

const (
	readFilesOpen     = "<read-files>"
	readFilesClose    = "</read-files>"
	modifiedFilesOpen = "<modified-files>"
	modifiedFilesEnd  = "</modified-files>"
)

// noteFiles adds the files the folded turns touched to the lists the run keeps. The lists accumulate
// across folds, so after the third one they still say what the whole session read and changed, which is
// what a summary of the last stretch alone could not.
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

// fileBlocks is the end of a recap: the files the session read and the files it changed, in blocks a
// resumed run can read back.
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

// recapOf reads a fold's message back: the summary without the file blocks, which are returned to the
// lists by restoreFiles instead of being summarised again.
func recapOf(m llm.Message) (string, bool) {
	text, ok := strings.CutPrefix(m.Content, FoldMark)
	if !ok || m.Role != "user" {
		return "", false
	}
	if at := strings.Index(text, "\n\n"+readFilesOpen); at >= 0 {
		text = text[:at]
	} else if at := strings.Index(text, "\n\n"+modifiedFilesOpen); at >= 0 {
		text = text[:at]
	}
	return strings.TrimSpace(text), true
}

// restoreFiles reads the file lists out of a resumed conversation's recap, so the next fold carries on
// from them rather than starting a list of its own.
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
