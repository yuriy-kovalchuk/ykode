package llm

import (
	"strings"
	"testing"
)

// a level goes to the server as the effort word it spells, and off as the word engines read as no thinking
func TestEveryThinkingLevelIsSentAsTheEffortWordItSpells(t *testing.T) {
	want := map[string]string{
		"off": "none", "minimal": "minimal", "low": "low", "medium": "medium",
		"high": "high", "xhigh": "xhigh", "max": "max",
	}
	for _, level := range ThinkingLevels() {
		if got := EffortFor(level); got != want[level] {
			t.Errorf("%q is sent as %q, want %q", level, got, want[level])
		}
	}
}

// a word a model refused gives way to the nearest one it takes, the deeper one on a tie
func TestARefusedEffortBecomesTheNearestWordTheModelTakes(t *testing.T) {
	qwen := []string{"none", "minimal", "low", "medium", "xhigh"} // what radiance names for Qwen3.8
	for _, tc := range []struct {
		want, got string
		supported []string
		refused   []string
	}{
		{"max", "xhigh", qwen, []string{"max"}},
		{"high", "xhigh", qwen, []string{"high"}},
		{"high", "xhigh", nil, []string{"high"}},           // nothing named: the ladder, deeper first
		{"high", "medium", nil, []string{"high", "xhigh"}}, // then the other side
		{"minimal", "low", []string{"low", "medium", "xhigh"}, []string{"minimal"}},
		{"low", "medium", nil, []string{"low"}},
	} {
		if got := NearestEffort(tc.want, tc.supported, tc.refused); got != tc.got {
			t.Errorf("%q refused with %v named and %v refused: got %q, want %q", tc.want, tc.supported, tc.refused, got, tc.got)
		}
	}
}

// a thinking level never falls back to none: that would turn off the thinking it asked for
func TestAThinkingLevelNeverFallsBackToNoThinking(t *testing.T) {
	if got := NearestEffort("low", []string{"none"}, []string{"low"}); got != "" {
		t.Errorf("low fell back to %q with only none left, want the field left out", got)
	}
	if got := NearestEffort("none", []string{"low", "medium"}, []string{"none"}); got != "" {
		t.Errorf("a refused none became %q, want the field left out so enable_thinking carries off alone", got)
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
