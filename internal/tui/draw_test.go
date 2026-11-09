package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// the session starts one row below the terminal's top edge, and the screen is still exactly as tall
// as the terminal: the row the margin takes comes off what the session reads
func TestTheSessionStartsBelowTheTopEdge(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := 0; i < 40; i++ {
		m.handle(agent.Event{Kind: agent.Answer, Text: "a line of the answer "})
	}
	m.refresh()

	rows := strings.Split(m.View(), "\n")
	if len(rows) != 24 {
		t.Fatalf("the screen is %d rows, want the terminal's 24", len(rows))
	}
	if rows[0] != "" {
		t.Errorf("the first row is not the margin: %q", rows[0])
	}
	if strings.TrimSpace(rows[1]) == "" {
		t.Errorf("the margin cost a row of the session as well:\n%.200s", m.View())
	}
}
