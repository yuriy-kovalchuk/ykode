package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// the session starts a row below the top edge, and the screen stays the terminal's height
func TestTheSessionStartsBelowTheTopEdge(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := 0; i < 40; i++ {
		m.handle(agent.Event{Kind: agent.Answer, Text: "a line of the answer "})
	}
	m.refresh()

	rows := strings.Split(m.render(), "\n")
	if len(rows) != 24 {
		t.Fatalf("the screen is %d rows, want the terminal's 24", len(rows))
	}
	if rows[0] != "" {
		t.Errorf("the first row is not the margin: %q", rows[0])
	}
	if strings.TrimSpace(rows[1]) == "" {
		t.Errorf("the margin cost a row of the session as well:\n%.200s", m.render())
	}
}
