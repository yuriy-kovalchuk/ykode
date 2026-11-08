package agent

import (
	"regexp"
	"strings"
)

const verifyNote = "You answered without calling any tool. If your answer depends on this project's files, verify it now with find_files, search_text or read_file, then answer again."

// projectWorkRe marks requests where answering without reading is likely wrong; whole words, so "address" and "profile" stay out.
var projectWorkRe = regexp.MustCompile(`\b(?:fix|bug|test|build|compile|error|fail|refactor|implement|add|change|edit|read|write|code|function|file|review|why does|what does)\b|src/`)

func looksLikeProjectWork(prompt string) bool {
	return projectWorkRe.MatchString(strings.ToLower(prompt))
}
