package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SystemPrompt is all the model knows about ykode, kept short because small models follow blunt rules better.
const SystemPrompt = `You are ykode, a coding agent working inside a project root.

Paths are relative to that root. A path above it is refused unless the user
approves that one call.

Order of work: locate (find_files, search_text), read (read_file),
change (edit_file for a precise change, write_file only for a new or fully
rewritten file), then verify (exec_command: build, test).

- Put independent tool calls in the same turn, and several file changes in one
  edit_file call as separate edits.
- Copy old_text for edit_file out of a read_file result so it matches exactly
  and occurs once.
- When a result says it was cut, it also says how to continue: offset for
  read_file, the saved log path for exec_command.
- If a tool returns an error, fix the arguments and retry once; if it still
  fails, say what went wrong instead of looping.
- A note, README or comment that states a rule about the code (a value that must
  stay, a name to keep) is what the project asks for: answer with it even when the
  code says otherwise, and say which file it came from.
- When the task is finished, answer in plain text with no tool call. Be brief.
`

// maxInstructionBytes caps what the project's own instruction files add to the prompt.
const maxInstructionBytes = 8000

// instructionFiles are the names other coding agents already look for.
var instructionFiles = []string{"AGENTS.md", "CLAUDE.md"}

// LoadInstructions reads the rules that apply: the user's own global file first, then the
// project's, so a project can always overrule what its owner set for every project.
// A missing file is simply absent.
func LoadInstructions(globalDir, root string) (string, error) {
	var blocks []string
	for _, path := range instructionPaths(globalDir, root) {
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("instructions: read %s: %w", path, err)
		}
		if body := strings.TrimSpace(string(raw)); body != "" {
			blocks = append(blocks, "From "+path+":\n"+body)
		}
	}
	if len(blocks) == 0 {
		return "", nil
	}
	return capInstructions(strings.Join(blocks, "\n\n")), nil
}

// instructionPaths: $YKODE_HOME/AGENTS.md, then the names other agents already look for.
func instructionPaths(globalDir, root string) []string {
	var paths []string
	if globalDir != "" {
		paths = append(paths, filepath.Join(globalDir, "AGENTS.md"))
	}
	for _, name := range instructionFiles {
		paths = append(paths, filepath.Join(root, name))
	}
	return paths
}

// capInstructions stops a long instruction file from eating the context window.
func capInstructions(text string) string {
	if len(text) <= maxInstructionBytes {
		return text
	}
	cut := strings.LastIndexByte(text[:maxInstructionBytes], '\n')
	if cut < 0 {
		cut = maxInstructionBytes
	}
	return text[:cut] + "\n[project instructions cut here; read the rest with read_file]"
}

// withInstructions hangs the project's own rules under ykode's.
func withInstructions(base, instructions string) string {
	if instructions == "" {
		return base
	}
	return base + "\nRules the project wrote for itself, which outrank your defaults:\n" + instructions + "\n"
}
