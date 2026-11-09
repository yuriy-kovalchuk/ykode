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

// noToolsRe is a request that says not to use tools: the verify nudge must not argue with it.
var noToolsRe = regexp.MustCompile(`\b(?:do not|don't|dont|without|no need to|never)\s+(?:use\s+|call\s+|run\s+)?(?:any\s+|the\s+)?tools?\b`)

func forbidsTools(prompt string) bool { return noToolsRe.MatchString(strings.ToLower(prompt)) }

// textCallRe finds a tool call written out as text, the way some chat templates make models write it
// when they were not offered the tool interface or lost track of it.
var textCallRe = regexp.MustCompile(`(?s)<tool_call>.*?(?:</tool_call>|$)|<function=[^>]*>.*?(?:</function>|$)`)

func hasTextCall(s string) bool { return textCallRe.MatchString(s) }

// withoutTextCalls removes the calls written as text, leaving what the model said around them.
func withoutTextCalls(s string) string {
	return strings.TrimSpace(textCallRe.ReplaceAllString(s, ""))
}
