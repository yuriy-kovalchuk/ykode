package cli

import (
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

func TestUsageLineShowsWhatTheConversationCost(t *testing.T) {
	line := usageLine(agent.Usage{
		Turns: 3, PromptTok: 1800, CachedTok: 1600, GenTok: 240, TotalPrompt: 5200,
		EstTok: 2000, BytesPerTok: 3.6,
	}, 131072)
	for _, want := range []string{"3 turns", "prompt 1800", "cached 1600", "generated 240", "~2000 of 131072", "3.6 bytes/token"} {
		if !strings.Contains(line, want) {
			t.Errorf("missing %q in: %s", want, line)
		}
	}
}

func TestUsageLineBeforeTheFirstRequest(t *testing.T) {
	if line := usageLine(agent.Usage{}, 131072); !strings.Contains(line, "nothing asked yet") {
		t.Errorf("line = %q, want it to say there is nothing to report", line)
	}
}

func TestUsageLineWithoutAKnownContextWindow(t *testing.T) {
	line := usageLine(agent.Usage{Turns: 1, PromptTok: 100, EstTok: 120, BytesPerTok: 3.3}, 0)
	if strings.Contains(line, "of 0") {
		t.Errorf("line = %q, want no context window mentioned when the server never reported one", line)
	}
}
