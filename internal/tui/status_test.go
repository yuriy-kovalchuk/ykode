package tui

import (
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

func stripCodes(s string) string {
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], "m")
		if j < 0 {
			return s
		}
		s = s[:i] + s[i+j+1:]
	}
}

func TestTheStatusLineSaysWhatTheSessionCosts(t *testing.T) {
	usage := agent.Usage{Turns: 3, PromptTok: 1000, CachedTok: 900, EstTok: 12000}
	line := statusLine("Qwen3.6-35B-A3B", usage, 131072, 41.6, true, 2)
	for _, want := range []string{"Qwen3.6-35B-A3B", "12.0k/131k", "cache 90%", "42 tok/s", "2 queued"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q missing from: %s", want, line)
		}
	}
}

func TestTheStatusLineKeepsQuietNumbersQuiet(t *testing.T) {
	line := statusLine("m", agent.Usage{}, 0, 0, false, 0)
	if strings.Contains(line, "tok/s") || strings.Contains(line, "cache") || strings.Contains(line, "ctx /") {
		t.Errorf("invented numbers before the server gave any: %s", line)
	}
	if !strings.Contains(line, "m") {
		t.Errorf("the model vanished: %s", line)
	}
}

// stripCodes removes the colour escapes so a test can measure what the terminal shows.
