package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// a transcript taller than the screen, scrolled to its end the way a live session is
func longSession(t *testing.T) *model {
	t.Helper()
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := 0; i < 60; i++ {
		m.handle(agent.Event{Kind: agent.Notice, Text: fmt.Sprintf("line %d of the session", i)})
	}
	if !m.view.AtBottom() {
		t.Fatal("a live session does not follow its own output")
	}
	return m
}

func wheel(b tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{Button: b, Action: tea.MouseActionPress}
}

// the wheel scrolls the transcript; it never reaches the input box
func TestTheWheelScrollsTheTranscriptNotTheHistory(t *testing.T) {
	m := longSession(t)
	m.history, m.histIdx = []string{"an earlier prompt"}, 1

	before := m.view.YOffset
	m.Update(wheel(tea.MouseButtonWheelUp))
	if m.view.YOffset != before-wheelStep {
		t.Errorf("one notch up moved the transcript from %d to %d, want %d", before, m.view.YOffset, before-wheelStep)
	}
	if got := m.input.Value(); got != "" {
		t.Errorf("the wheel put %q in the input box", got)
	}
	m.Update(wheel(tea.MouseButtonWheelDown))
	if m.view.YOffset != before {
		t.Errorf("one notch down did not come back: %d, want %d", m.view.YOffset, before)
	}
}

// the arrows still walk the history: only the wheel changed meaning
func TestTheArrowsStillWalkTheHistory(t *testing.T) {
	m := longSession(t)
	m.history, m.histIdx = []string{"an earlier prompt"}, 1
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.input.Value(); got != "an earlier prompt" {
		t.Errorf("up with an empty box gave %q, want the earlier prompt", got)
	}
}

// scrolled up, the screen says where the reader is and when output arrived below
func TestScrolledUpTheScreenSaysSoAndMarksNewOutput(t *testing.T) {
	m := longSession(t)
	for i := 0; i < 5; i++ {
		m.Update(wheel(tea.MouseButtonWheelUp))
	}
	if !strings.Contains(stripCodes(m.View()), "↑") || !strings.Contains(stripCodes(m.View()), "end returns") {
		t.Errorf("scrolled up, the screen does not say so:\n%s", stripCodes(m.bottomArea()))
	}
	offset := m.view.YOffset
	m.handle(agent.Event{Kind: agent.Notice, Text: "something new"})
	if m.view.YOffset != offset {
		t.Error("new output pulled a reader who scrolled up back to the end")
	}
	if !strings.Contains(stripCodes(m.View()), "new output below") {
		t.Errorf("new output below a scrolled-up reader is not marked:\n%s", stripCodes(m.bottomArea()))
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !m.view.AtBottom() || m.unseen {
		t.Error("end with an empty box did not return to the end and clear the mark")
	}
	if strings.Contains(stripCodes(m.View()), "end returns") {
		t.Error("back at the end, the screen still says the reader is scrolled up")
	}
}

// ctrl+t hands the mouse to the terminal for selecting text, and takes it back
func TestCtrlTHandsTheMouseBackForSelecting(t *testing.T) {
	m := longSession(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if !m.mouseOff || cmd == nil {
		t.Fatal("ctrl+t did not release the mouse")
	}
	before := m.view.YOffset
	m.Update(wheel(tea.MouseButtonWheelUp))
	if m.view.YOffset != before {
		t.Error("with the mouse released, a wheel event still scrolled")
	}
	if _, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlT}); m.mouseOff || cmd == nil {
		t.Error("a second ctrl+t did not take the mouse back")
	}
}

// the screen is exactly the terminal's height: nothing wraps into a row the layout did not count
func TestTheScreenFillsTheTerminalExactly(t *testing.T) {
	for _, width := range []int{40, 80, 200} {
		m := newTestModel()
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		for i := 0; i < 60; i++ {
			m.handle(agent.Event{Kind: agent.Notice, Text: "a line"})
		}
		m.err = "the server went away"
		m.refresh()
		if h := lipgloss.Height(m.View()); h != 24 {
			t.Errorf("at width %d the screen is %d rows, want 24", width, h)
		}
		for i, line := range strings.Split(m.View(), "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("at width %d, row %d is %d wide", width, i, w)
			}
		}
	}
}
