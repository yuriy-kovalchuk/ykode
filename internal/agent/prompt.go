package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SystemPrompt is pi's shape: who ykode is, its tools in a line each, and a few plain guidelines.
// Task-specific rules (verify once, answer only the value, follow a project file's rule) were
// tried and each fixed one benchmark task while breaking another; this prompt passed 55/55 on
// Qwen3.6 against 54/55 for those rules, 8% faster, with 23% fewer prompt tokens (2026-11-08).
// A rule that comes back has to earn it with a measured failure this prompt has and pi does not.
const SystemPrompt = `You are ykode, a coding assistant working in one project. Help the user inspect files,
run commands, and change code.

Tools:
- find_files: find files by name or list a directory
- search_text: search file contents
- read_file: read a file
- edit_file: precise edits by exact text replacement, several in one call
- write_file: create a file or rewrite one completely
- exec_command: run a shell command

Guidelines:
- Use find_files and search_text to locate code, read_file to read it, instead of shell commands.
- Use edit_file for targeted changes; batch changes to one file in a single call.
  Each old_text must match the file exactly and only once.
- Use write_file only for new files or complete rewrites.
- Be concise in your responses.
- Show file paths clearly when working with files.
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
