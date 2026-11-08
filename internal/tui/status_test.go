package tui

import (
	"github.com/charmbracelet/lipgloss"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"context"
	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
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
	usage := agent.Usage{Turns: 3, PromptTok: 1000, CachedTok: 900, GenTok: 17000,
		TotalPrompt: 90000, CachedTotal: 3100000, EstTok: 12000}
	line := stripCodes(statusLine(status{usage: usage, ctxSize: 131072, decode: 41.6, running: true, queued: 2}))
	for _, want := range []string{"↑90k", "↓17k", "R3.1M", "cache 90%", "12k/131k", "42 tok/s", "2 queued"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q missing from: %s", want, line)
		}
	}
}

// the row above the costs says where the session runs and which model and depth answer
func TestTheSessionLineNamesTheProjectTheModelAndTheDepth(t *testing.T) {
	wide := stripCodes(sessionLine(status{root: "/home/user/project", branch: "main",
		model: "Qwen3.6-35B-A3B", thinking: "high", width: 80}))
	for _, want := range []string{"/home/user/project (main)", "Qwen3.6-35B-A3B", "high"} {
		if !strings.Contains(wide, want) {
			t.Errorf("%q missing from: %s", want, wide)
		}
	}
	if lipgloss.Width(wide) != 80 {
		t.Errorf("the row is %d wide on an 80-wide screen: %q", lipgloss.Width(wide), wide)
	}

	// outside a repository the path stands alone, and a model with no thinking says so
	plain := stripCodes(sessionLine(status{root: "/home/user/project", model: "m", thinking: "off", width: 80}))
	if strings.Contains(plain, "(") || !strings.Contains(plain, "thinking off") {
		t.Errorf("no repository or no thinking is drawn wrong: %q", plain)
	}
}

// a path under the user's home is short enough to sit beside the branch and the model
func TestTheSessionsPathIsWrittenFromHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("this machine has no home directory")
	}
	if got := shortenHome(filepath.Join(home, "Developer", "ykode")); got != "~/Developer/ykode" {
		t.Errorf("shortenHome = %q, want ~/Developer/ykode", got)
	}
	if got := shortenHome(home); got != "~" {
		t.Errorf("shortenHome(home) = %q, want ~", got)
	}
	if got := shortenHome("/srv/work/project"); got != "/srv/work/project" {
		t.Errorf("a path outside home is rewritten: %q", got)
	}
}

// 3.1M reads at a glance; the exact figure is on /usage
func TestTokenCountsAreWrittenToBeRead(t *testing.T) {
	for _, want := range []struct {
		n    int
		said string
	}{{900, "900"}, {9500, "9.5k"}, {12000, "12k"}, {131072, "131k"}, {3100000, "3.1M"}, {12300000, "12M"}} {
		if got := tokCount(want.n); got != want.said {
			t.Errorf("tokCount(%d) = %q, want %q", want.n, got, want.said)
		}
	}
}

func TestTheStatusLineKeepsQuietNumbersQuiet(t *testing.T) {
	line := stripCodes(statusLine(status{}))
	if strings.Contains(line, "tok/s") || strings.Contains(line, "cache") || strings.Contains(line, "ctx /") ||
		strings.Contains(line, "draft") || strings.Contains(line, "↑") || strings.Contains(line, "↓") ||
		strings.Contains(line, "R") {
		t.Errorf("invented numbers before the server gave any: %s", line)
	}
	if !strings.Contains(line, "ctx") {
		t.Errorf("the window vanished: %s", line)
	}
}

// stripCodes removes the colour escapes so a test can measure what the terminal shows.

func TestTheStatusLineShowsWhatTheDraftModelSaved(t *testing.T) {
	u := agent.Usage{Turns: 1, EstTok: 1000, PromptTok: 100, Draft: 200, DraftOK: 120, Acceptance: 0.6}
	line := stripCodes(statusLine(status{usage: u, ctxSize: 8000, decode: 40}))
	if !strings.Contains(line, "draft 60%") {
		t.Errorf("no acceptance in: %s", line)
	}
}

// both speeds the server reports are on the line: how fast it read the prompt, and how fast it wrote
func TestTheStatusLineShowsPrefillAndDecodeSpeed(t *testing.T) {
	line := stripCodes(statusLine(status{prefill: 512.4, decode: 33.2}))
	if !strings.Contains(line, "↑ 512") || !strings.Contains(line, "↓ 33 tok/s") {
		t.Errorf("speeds missing from: %s", line)
	}
}

// while it works the line counts the time; once done it says how long the prompt took
func TestTheStatusLineTimesThePrompt(t *testing.T) {
	line := stripCodes(statusLine(status{running: true, elapsed: 72 * time.Second, spinner: "⠋"}))
	if !strings.Contains(line, "⠋ 1m12s") {
		t.Errorf("no running timer in: %s", line)
	}
	line = stripCodes(statusLine(status{took: 42 * time.Second}))
	if !strings.Contains(line, "done in 42s") {
		t.Errorf("no time for the finished prompt in: %s", line)
	}
	for d, want := range map[time.Duration]string{7 * time.Second: "7s", 3723 * time.Second: "1h02m"} {
		if got := duration(d); got != want {
			t.Errorf("duration(%v) = %q, want %q", d, got, want)
		}
	}
}

// facts sit on the left and what is happening now on the right; a narrow screen drops the oldest
// fact first, and how full the window is is the last thing it takes
func TestTheStatusLineKeepsActivityWhenNarrow(t *testing.T) {
	s := status{usage: agent.Usage{Turns: 1, EstTok: 3000, PromptTok: 100, CachedTok: 50, TotalPrompt: 90000, GenTok: 17000},
		ctxSize: 131072, running: true, elapsed: 5 * time.Second, spinner: "⠋", decode: 40, width: 100}
	wide := stripCodes(statusLine(s))
	if lipgloss.Width(wide) != 100 || !strings.HasPrefix(wide, "↑90k") || !strings.HasSuffix(wide, "40 tok/s") {
		t.Errorf("not spread across the width: %q", wide)
	}
	for _, width := range []int{45, 30} {
		s.width = width
		narrow := stripCodes(statusLine(s))
		if !strings.Contains(narrow, "⠋ 5s") || lipgloss.Width(narrow) > width {
			t.Errorf("at %d the line lost the activity or overflowed: %q", width, narrow)
		}
	}
	s.width = 45
	middle := stripCodes(statusLine(s))
	if strings.Contains(middle, "↑90k") || !strings.Contains(middle, "ctx") {
		t.Errorf("at 45 the totals went before the window: %q", middle)
	}
}

// the footer names the project the agent works in, and the depth it thinks at
func TestTheFooterReadsWhatTheAgentKnows(t *testing.T) {
	dir := t.TempDir()
	a := agent.New(nil, nil, "test-model", agent.Options{Root: dir, ThinkingLevel: "high"}, nil)
	m := newModel(context.Background(), a, NewHub())
	s := m.status()
	if s.root != dir || s.thinking != "high" || s.model != "test-model" {
		t.Errorf("the footer reports %+v, want the agent's root, model and depth", s)
	}
}

// the footer reads the agent's numbers as it draws, so a command that changed the conversation shows
// on the next frame rather than on the next reply
func TestTheFooterReadsTheAgentsNumbersAtDrawTime(t *testing.T) {
	a := agent.New(nil, nil, "test-model", agent.Options{CtxSize: 8000}, nil)
	m := newModel(context.Background(), a, NewHub())
	before := stripCodes(statusLine(m.status()))
	a.Resume([]llm.Message{{Role: "user", Content: strings.Repeat("a call, ", 600)}})
	if after := stripCodes(statusLine(m.status())); after == before {
		t.Errorf("the footer kept its numbers after the conversation grew:\n%s\n%s", before, after)
	}
}
