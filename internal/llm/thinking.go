package llm

import (
	"fmt"
	"strings"
)

// Thinking levels, named as pi names them so the same word means the same depth on both agents.
const (
	ThinkingOff          = "off"
	DefaultThinkingLevel = "medium"
)

// thinkingBudgetTok is what llama-server's thinking_budget_tokens gets per level; max and off name none
var thinkingBudgetTok = map[string]int{
	ThinkingOff: 0, "minimal": 1024, "low": 2048, DefaultThinkingLevel: 8192,
	"high": 16384, "xhigh": 32768, "max": 0,
}

// ThinkingBudgetTokens reports the budget a level asks the server for, and whether ykode knows the level.
func ThinkingBudgetTokens(level string) (budget int, known bool) {
	budget, known = thinkingBudgetTok[level]
	return budget, known
}

// ThinkingLevels lists the levels shallowest first, for help text and for an error that names them.
func ThinkingLevels() []string {
	return []string{ThinkingOff, "minimal", "low", DefaultThinkingLevel, "high", "xhigh", "max"}
}

// ThinkingLevelValue checks a level the user typed and names the alternatives when it is not one.
func ThinkingLevelValue(level string) (string, error) {
	if _, known := thinkingBudgetTok[level]; !known {
		return "", fmt.Errorf("want one of %s", strings.Join(ThinkingLevels(), ", "))
	}
	return level, nil
}
