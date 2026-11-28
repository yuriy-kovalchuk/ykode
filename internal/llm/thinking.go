package llm

import (
	"fmt"
	"slices"
	"strings"
)

// Thinking levels, named as pi names them; each goes to the server as the reasoning_effort word it spells.
const (
	ThinkingOff          = "off"
	DefaultThinkingLevel = "medium"
	EffortNone           = "none" // the word off is sent as: llama.cpp, strata and radiance all read it as no thinking
	MinAnswerTokens      = 1024   // what the answer keeps of a reply once thinking takes its share
)

// thinkingLevels are shallowest first; the effort words they send line up with them one for one
var thinkingLevels = []string{ThinkingOff, "minimal", "low", DefaultThinkingLevel, "high", "xhigh", "max"}

// ThinkingLevels lists the levels shallowest first, for help text and for an error that names them.
func ThinkingLevels() []string { return slices.Clone(thinkingLevels) }

// ThinkingLevelValue checks a level the user typed and names the alternatives when it is not one.
func ThinkingLevelValue(level string) (string, error) {
	if !slices.Contains(thinkingLevels, level) {
		return "", fmt.Errorf("want one of %s", strings.Join(thinkingLevels, ", "))
	}
	return level, nil
}

// EffortFor is the reasoning_effort word a level is sent as.
func EffortFor(level string) string {
	if level == ThinkingOff {
		return EffortNone
	}
	return level
}

// effortRank places a word on the levels' ladder; -1 for a word ykode does not know
func effortRank(word string) int {
	if word == EffortNone {
		return 0
	}
	return slices.Index(thinkingLevels, word)
}

// NearestEffort is the word closest to want that a model may still take, the deeper one on a tie; "" is none
func NearestEffort(want string, supported, refused []string) string {
	if want == EffortNone {
		return "" // no other word means no thinking: enable_thinking carries off on its own
	}
	candidates := supported
	if len(candidates) == 0 {
		candidates = thinkingLevels[1:] // nothing named: try the ladder, off left out
	}
	rank, best, bestGap := effortRank(want), "", 0
	for _, word := range candidates {
		r := effortRank(word)
		// a server's own list may name none, and a thinking level never becomes it: that turns thinking off
		if r <= 0 || slices.Contains(refused, word) {
			continue
		}
		gap := abs(r - rank)
		if best == "" || gap < bestGap || (gap == bestGap && r > effortRank(best)) {
			best, bestGap = word, gap
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
