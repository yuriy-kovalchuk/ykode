package llm

import (
	"strings"
	"testing"
)

// the depths are named as pi names them, so one word means the same amount of thinking on both agents
func TestEveryThinkingLevelCarriesTheBudgetPiGivesIt(t *testing.T) {
	want := map[string]int{
		"off": 0, "minimal": 1024, "low": 2048, "medium": 8192,
		"high": 16384, "xhigh": 32768, "max": 0,
	}
	for level, tokens := range want {
		got, known := ThinkingBudgetTokens(level)
		if !known {
			t.Errorf("%q is not a thinking level", level)
			continue
		}
		if got != tokens {
			t.Errorf("%q asks for %d thinking tokens, want %d", level, got, tokens)
		}
	}
}

// "max" sends no budget at all: the model thinks until the reply itself ends
func TestTheMaxLevelAsksTheServerForNothing(t *testing.T) {
	budget, known := ThinkingBudgetTokens("max")
	if !known {
		t.Fatal("max should be a level")
	}
	if budget != 0 {
		t.Errorf("max budget = %d: the field must be left out", budget)
	}
}

func TestAnInventedThinkingLevelIsNamed(t *testing.T) {
	_, err := ThinkingLevelValue("enormous")
	if err == nil {
		t.Fatal("an invented level was accepted")
	}
	for _, level := range ThinkingLevels() {
		if !strings.Contains(err.Error(), level) {
			t.Fatalf("the error does not list %q: %v", level, err)
		}
	}
}

func TestTheThinkingLevelsAreListedInOrder(t *testing.T) {
	levels := ThinkingLevels()
	if levels[0] != "off" || levels[len(levels)-1] != "max" {
		t.Errorf("levels = %v: want off first and max last", levels)
	}
	if DefaultThinkingLevel != "medium" {
		t.Errorf("default level = %q, want medium, which is what pi runs", DefaultThinkingLevel)
	}
}
