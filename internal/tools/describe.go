package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/size"
)

// describeChars is how much of a command fits in a description before it is cut.
const describeChars = 120

// Describe says what a call asks for in the words a person reads before deciding: the file, the
// pattern, the command. The JSON is what the model wrote; this is the screen's copy.
func Describe(name, args string) string {
	switch name {
	case "read_file":
		return describeWith(args, func(in readInput) string {
			where := in.Path
			if in.Offset > 0 {
				where += fmt.Sprintf(":%d", in.Offset)
				if in.Limit > 0 {
					where += fmt.Sprintf("-%d", in.Offset+in.Limit-1)
				}
			}
			return where
		})
	case "write_file":
		return describeWith(args, func(in writeInput) string {
			return fmt.Sprintf("%s · %d lines · %s", in.Path, lines(in.Content), size.Text(len(in.Content)))
		})
	case "edit_file":
		return describeWith(args, editShape)
	case "exec_command":
		return describeWith(args, func(in struct {
			Command string `json:"command"`
		}) string {
			return oneLine(strings.Join(strings.Fields(in.Command), " "), describeChars)
		})
	case "plan":
		return describeWith(args, planShape)
	case "ask_user":
		return describeWith(args, choiceShape)
	}
	return brief(args)
}

// describeWith reads a call's arguments as T and words them; arguments that cannot be read are shown
// as they came, cut short.
func describeWith[T any](args string, words func(T) string) string {
	var in T
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return brief(args)
	}
	return words(in)
}

// editShape is a call that changes a file: how many places, and how the lines move.
func editShape(in editInput) string {
	pairs := []editPair{}
	if len(in.Edits) > 0 && json.Unmarshal(in.Edits, &pairs) != nil {
		return fmt.Sprintf("%s · %s", in.Path, brief(string(in.Edits)))
	}
	if len(pairs) == 0 && (in.OldText != "" || in.OldString != "" || in.NewText != nil || in.NewString != nil) {
		pairs = []editPair{{OldText: in.OldText, OldString: in.OldString, NewText: in.NewText, NewString: in.NewString}}
	}
	if len(pairs) == 0 {
		return in.Path
	}
	old, added := 0, 0
	for _, p := range pairs {
		old += lines(firstNonEmpty(p.OldText, p.OldString))
		if text, ok := pairNew(p); ok {
			added += lines(text)
		}
	}
	if len(pairs) == 1 {
		return fmt.Sprintf("%s · %d lines → %d", in.Path, old, added)
	}
	return fmt.Sprintf("%s · %d edits · %d lines → %d", in.Path, len(pairs), old, added)
}

// pairNew is what an edit puts in place; a missing new_text is a slip, an empty one a deletion.
func pairNew(p editPair) (string, bool) {
	if p.NewText != nil {
		return *p.NewText, true
	}
	if p.NewString != nil {
		return *p.NewString, true
	}
	return "", false
}

// lines counts lines the way an editor does: empty text has none, a closing newline ends a line.
func lines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
}

// brief is the fallback for a call this package does not know, or arguments it cannot read.
func brief(args string) string {
	return oneLine(strings.Join(strings.Fields(args), " "), describeChars)
}
