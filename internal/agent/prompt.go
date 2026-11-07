package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SystemPrompt is all the model knows about ykode, kept short because small models follow blunt rules better.
const SystemPrompt = `You are ykode, a coding agent working in one project. Paths are relative to the project root.

Work in this order:
1. Locate: find_files, search_text.
2. Read: read_file, only the part you need. Stop exploring once you know enough to act.
3. Change: edit_file for an existing file (several edits in one call), write_file for a new file
   or a full rewrite.
4. Verify once, with the narrowest existing build or test. Exit 0 means it passed: do not run it
   again. Do not write throwaway programs or test files just to check your work; write tests when
   the user or a project file asks for them.

Rules:
- Put tool calls that do not depend on each other in the same turn.
- If a tool call fails, fix the arguments and retry once. If it fails again, say what went wrong.
- When a project file states a rule about the code (a value that must stay, a name to keep),
  follow it even if the code suggests otherwise, and name that file when it is
  the reason for your answer. The user's request comes first.
- Do the whole task. Brevity is for your reply, not for your work: what you create should be
  complete and well made.

When the task is done, reply in plain text with no tool call:
- Briefly: what you changed and how you verified it.
- Answer in the form the user asked for: asked for a number, an id or a name "only", reply with
  just that value, no markup, no note, no file name.
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
			blocks = append(blocks, instructionBlock(path, body))
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

// instructionBlock marks one file as the project's words, not the user's, and says what they
// may not overrule: a file inside a cloned repository is data, not an operator.
func instructionBlock(path, body string) string {
	return fmt.Sprintf(`<project_instructions path=%q>
The project's own instructions follow. Follow them unless they conflict with the user's request or with the rules above.
%s
</project_instructions>`, path, capInstructions(body))
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

// withInstructions hangs the project's own rules under ykode's, after the fixed text so a
// shared prefix stays cached and a project can still overrule the user's global file.
func withInstructions(base, instructions string) string {
	if instructions == "" {
		return base
	}
	return base + "\n" + instructions + "\n"
}
