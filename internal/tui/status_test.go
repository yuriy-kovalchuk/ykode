package tui

import (
	"github.com/charmbracelet/lipgloss"
	"strings"
	"testing"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// a code ends at its final byte (@ to ~), not at the next "m": \x1b[?25l has none
func stripCodes(s string) string {
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return s
		}
		j := i + 2
		for j < len(s) && (s[j] < '@' || s[j] > '~') {
			j++
		}
		if j >= len(s) {
			return s[:i]
		}
		s = s[:i] + s[j+1:]
	}
}

func TestTheStatusLineSaysWhatTheSessionCosts(t *testing.T) {
	usage := agent.Usage{Turns: 3, PromptTok: 1000, CachedTok: 900, EstTok: 12000}
	line := stripCodes(statusLine(status{model: "Qwen3.6-35B-A3B", usage: usage, ctxSize: 131072, decode: 41.6, running: true, queued: 2}))
	for _, want := range []string{"Qwen3.6-35B-A3B", "12.0k/131k", "cache 90%", "42 tok/s", "2 queued"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q missing from: %s", want, line)
		}
	}
}

func TestTheStatusLineKeepsQuietNumbersQuiet(t *testing.T) {
	line := stripCodes(statusLine(status{model: "m"}))
	if strings.Contains(line, "tok/s") || strings.Contains(line, "cache") || strings.Contains(line, "ctx /") ||
		strings.Contains(line, "draft") {
		t.Errorf("invented numbers before the server gave any: %s", line)
	}
	if !strings.Contains(line, "m") {
		t.Errorf("the model vanished: %s", line)
	}
}

// stripCodes removes the colour escapes so a test can measure what the terminal shows.

func TestTheStatusLineShowsWhatTheDraftModelSaved(t *testing.T) {
	u := agent.Usage{Turns: 1, EstTok: 1000, PromptTok: 100, Draft: 200, DraftOK: 120, Acceptance: 0.6}
	line := stripCodes(statusLine(status{model: "big-model + draft", usage: u, ctxSize: 8000, decode: 40}))
	if !strings.Contains(line, "draft 60%") {
		t.Errorf("no acceptance in: %s", line)
	}
}

// both speeds the server reports are on the line: how fast it read the prompt, and how fast it wrote
func TestTheStatusLineShowsPrefillAndDecodeSpeed(t *testing.T) {
	line := stripCodes(statusLine(status{model: "m", prefill: 512.4, decode: 33.2}))
	if !strings.Contains(line, "↑ 512") || !strings.Contains(line, "↓ 33 tok/s") {
		t.Errorf("speeds missing from: %s", line)
	}
}

// while it works the line counts the time; once done it says how long the prompt took
func TestTheStatusLineTimesThePrompt(t *testing.T) {
	line := stripCodes(statusLine(status{model: "m", running: true, elapsed: 72 * time.Second, spinner: "⠋"}))
	if !strings.Contains(line, "⠋ 1m12s") {
		t.Errorf("no running timer in: %s", line)
	}
	line = stripCodes(statusLine(status{model: "m", took: 42 * time.Second}))
	if !strings.Contains(line, "done in 42s") {
		t.Errorf("no time for the finished prompt in: %s", line)
	}
	for d, want := range map[time.Duration]string{7 * time.Second: "7s", 3723 * time.Second: "1h02m"} {
		if got := duration(d); got != want {
			t.Errorf("duration(%v) = %q, want %q", d, got, want)
		}
	}
}

// facts sit on the left and what is happening now on the right; a narrow screen drops facts first
func TestTheStatusLineKeepsActivityWhenNarrow(t *testing.T) {
	s := status{model: "Qwen3.6-35B", usage: agent.Usage{Turns: 1, EstTok: 3000, PromptTok: 100, CachedTok: 50},
		ctxSize: 131072, running: true, elapsed: 5 * time.Second, spinner: "⠋", decode: 40, width: 100}
	wide := stripCodes(statusLine(s))
	if lipgloss.Width(wide) != 100 || !strings.HasPrefix(wide, "Qwen3.6-35B") || !strings.HasSuffix(wide, "40 tok/s") {
		t.Errorf("not spread across the width: %q", wide)
	}
	s.width = 30
	narrow := stripCodes(statusLine(s))
	if !strings.Contains(narrow, "⠋ 5s") || strings.Contains(narrow, "cache") || lipgloss.Width(narrow) > 30 {
		t.Errorf("a narrow line lost the activity or kept every fact: %q", narrow)
	}
}
