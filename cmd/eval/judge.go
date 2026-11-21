package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// contains asks about the prose the model finished with.
func (t *transcript) contains(needle string) bool { return containsFold(t.answer, needle) }

// sawDiff checks that an edit came back with a diff body.
func (t *transcript) sawDiff() bool {
	for _, result := range t.results {
		if strings.Contains(result, "@@") {
			return true
		}
	}
	return false
}

// countTool is how often a tool ran, which is what tells a re-read from an answer kept in mind.
func (t *transcript) countTool(name string) int {
	n := 0
	for _, tool := range t.tools {
		if tool == name {
			n++
		}
	}
	return n
}

func (t *transcript) ranTool(name string) bool {
	for _, tool := range t.tools {
		if tool == name {
			return true
		}
	}
	return false
}

// describe is what the model did, step by step, for tuning runs and failures.
func (t *transcript) describe() string {
	var b strings.Builder
	for i, tool := range t.tools {
		fmt.Fprintf(&b, "%d. %s\n", i+1, tool)
		if i < len(t.results) && t.results[i] != "" {
			fmt.Fprintf(&b, "   -> %s\n", flat(t.results[i], 160))
		}
	}
	for _, why := range t.failures {
		fmt.Fprintf(&b, "FAILED %s\n", flat(why, 160))
	}
	for _, note := range t.notice {
		fmt.Fprintf(&b, "NOTICE %s\n", flat(note, 160))
	}
	if t.answer != "" {
		b.WriteString("answer: " + flat(t.answer, 240))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (t *transcript) failedWith(needle string) bool {
	for _, why := range t.failures {
		if containsFold(why, needle) {
			return true
		}
	}
	return false
}

// evalResult is one task's outcome.
type evalResult struct {
	name      string
	passed    bool
	turns     int
	prompt    int
	generated int
	seconds   float64
	note      string
	story     string
	dir       string
	counters  map[string]int
}

// counters are the behaviours the prompt work targets, counted from what the run emitted.
func (t *transcript) counters(askErr error) map[string]int {
	c := map[string]int{
		"rerun":      t.reruns(),
		"compaction": t.noticed("summarise the older turns"),
		"drop":       t.noticed("to fit the context"),
		"overflow":   t.noticed("does not fit its window"),
		"stall":      t.noticed("stopped after thinking"),
		"repeat":     t.noticed("kept repeating"),
		"cut":        t.noticed("hit max_tokens"),
		"text-call":  t.noticed("wrote a tool call as text"),
		"no-text":    t.noticed("returned no text"),
	}
	if toolCallAsText.MatchString(t.answer) {
		c["call-as-text"] = 1
	}
	if strings.Count(strings.TrimSpace(t.answer), "\n") >= 5 {
		c["long-answer"] = 1
	}
	if askErr != nil && strings.Contains(askErr.Error(), "max-turns") {
		c["max-turns"] = 1
	}
	return c
}

func (t *transcript) noticed(needle string) int {
	n := 0
	for _, note := range t.notice {
		if strings.Contains(note, needle) {
			n++
		}
	}
	return n
}

// reruns counts commands run again with the same arguments, which the context already held.
func (t *transcript) reruns() int {
	seen, n := map[string]bool{}, 0
	for i, tool := range t.tools {
		if tool != "exec_command" || i >= len(t.args) {
			continue
		}
		if seen[t.args[i]] {
			n++
		}
		seen[t.args[i]] = true
	}
	return n
}

// toolCallAsText catches a call written into the answer instead of made: ykode then sees prose and stops.
var toolCallAsText = regexp.MustCompile(`<tool_call>|\{\s*"name"\s*:`)

func quote(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		s = s[:120] + "..."
	}
	return strconv.Quote(s)
}

func firstLineWith(body, needle string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, needle) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
