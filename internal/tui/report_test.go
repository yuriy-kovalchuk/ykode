package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

// /ctx and /usage stay on screen, with the window's fullness drawn as a bar, not a sentence
func TestCtxOpensTheFiguresPanel(t *testing.T) {
	a := agent.New(nil, nil, "test-model", agent.Options{CtxSize: 32768}, nil)
	m := newModel(context.Background(), a, NewHub(), config.Config{})
	m.Update(tea.WindowSizeMsg{Width: 92, Height: 24})
	m.input.SetValue("/ctx")
	m.submit()

	if m.picker == nil {
		t.Fatal("/ctx did not open the panel")
	}
	view := stripCodes(m.render())
	for _, want := range []string{"ykode · this session", "window", "32k tokens", "tool descriptions"} {
		if !strings.Contains(view, want) {
			t.Errorf("the panel lacks %q:\n%s", want, view)
		}
	}
	m.Update(esc)
	if m.picker != nil {
		t.Error("esc did not give the session back")
	}
}

// a window with nothing in it is an empty bar, a full one is all bar, and nothing of nothing is nothing
func TestABarShowsTheShareThatIsUsed(t *testing.T) {
	if empty := bar(0, 10); strings.Contains(empty, planCellDone) || strings.Count(empty, planCellRest) != 10 {
		t.Errorf("an empty window drew %q", empty)
	}
	if full := bar(100, 10); strings.Count(full, planCellDone) != 10 {
		t.Errorf("a full window drew %q", full)
	}
	if half := bar(50, 10); strings.Count(half, planCellDone) != 5 {
		t.Errorf("a half-full window drew %q", half)
	}
	if over := bar(140, 10); strings.Count(over, planCellDone) != 10 {
		t.Errorf("a window over its budget drew %q", over)
	}
	if percent(3, 0) != 0 || percent(1, 4) != 25 {
		t.Errorf("percent(3,0) = %d, percent(1,4) = %d", percent(3, 0), percent(1, 4))
	}
}
