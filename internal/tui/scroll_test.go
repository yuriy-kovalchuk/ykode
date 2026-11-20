package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
	if !m.view.atBottom() {
		t.Fatal("a live session does not follow its own output")
	}
	return m
}

func wheel(b tea.MouseButton) tea.MouseMsg {
	return tea.MouseWheelMsg{Button: b}
}

// the wheel scrolls the transcript; it never reaches the input box
func TestTheWheelScrollsTheTranscriptNotTheHistory(t *testing.T) {
	m := longSession(t)
	m.history, m.histIdx = []string{"an earlier prompt"}, 1

	before := m.view.at
	m.Update(wheel(tea.MouseWheelUp))
	if m.view.at != before-wheelStep {
		t.Errorf("one notch up moved the transcript from %d to %d, want %d", before, m.view.at, before-wheelStep)
	}
	if got := m.input.Value(); got != "" {
		t.Errorf("the wheel put %q in the input box", got)
	}
	m.Update(wheel(tea.MouseWheelDown))
	if m.view.at != before {
		t.Errorf("one notch down did not come back: %d, want %d", m.view.at, before)
	}
}

// the arrows and the wheel move the session; earlier prompts come from ctrl+p, never a scroll
func TestScrollingStaysScrollingAndCtrlPWalksTheHistory(t *testing.T) {
	m := longSession(t)
	m.history, m.histIdx = []string{"an earlier prompt"}, 1

	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.input.Value(); got != "" {
		t.Errorf("an arrow wrote into the box: %q", got)
	}
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if got := m.input.Value(); got != "an earlier prompt" {
		t.Errorf("ctrl+p gave %q, want the earlier prompt", got)
	}
}

// scrolled up, the screen says where the reader is and when output arrived below
func TestScrolledUpTheScreenSaysSoAndMarksNewOutput(t *testing.T) {
	m := longSession(t)
	for i := 0; i < 5; i++ {
		m.Update(wheel(tea.MouseWheelUp))
	}
	if !strings.Contains(stripCodes(m.render()), "↑") || !strings.Contains(stripCodes(m.render()), "end returns") {
		t.Errorf("scrolled up, the screen does not say so:\n%s", stripCodes(m.bottomArea()))
	}
	offset := m.view.at
	m.handle(agent.Event{Kind: agent.Notice, Text: "something new"})
	if m.view.at != offset {
		t.Error("new output pulled a reader who scrolled up back to the end")
	}
	if !strings.Contains(stripCodes(m.render()), "new output below") {
		t.Errorf("new output below a scrolled-up reader is not marked:\n%s", stripCodes(m.bottomArea()))
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if !m.view.atBottom() || m.unseen {
		t.Error("end with an empty box did not return to the end and clear the mark")
	}
	if strings.Contains(stripCodes(m.render()), "end returns") {
		t.Error("back at the end, the screen still says the reader is scrolled up")
	}
}

// the screen never asks for the pointer, so a wheel report is a leftover mode: it still scrolls
func TestAPointerReportNeverWritesIntoTheBox(t *testing.T) {
	m := longSession(t)
	m.input.SetValue("a prompt half typed")
	before := m.view.at
	for _, msg := range []tea.MouseMsg{
		tea.MouseClickMsg{Button: tea.MouseLeft},
		tea.MouseReleaseMsg{Button: tea.MouseLeft},
		tea.MouseMotionMsg{Button: tea.MouseLeft},
		tea.MouseReleaseMsg{Button: tea.MouseWheelUp},
	} {
		if _, cmd := m.Update(msg); cmd != nil {
			t.Errorf("a pointer report asked for a command: %v", msg)
		}
		if got := m.input.Value(); got != "a prompt half typed" {
			t.Errorf("pointer report %v changed the box to %q", msg, got)
		}
		if m.view.at != before {
			t.Errorf("pointer report %v moved the transcript", msg)
		}
	}
	// while an overlay holds a text box, the same rule holds: the wheel scrolls the session under it
	m.scrollWithOverlayOpen(t, wheel(tea.MouseWheelUp))
}

// scrollWithOverlayOpen checks the wheel reaches the transcript under an open list, typing nothing
func (m *model) scrollWithOverlayOpen(t *testing.T, msg tea.MouseMsg) {
	t.Helper()
	m.input.SetValue("/settings")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.screen == nil {
		t.Fatal("/settings did not open the screen")
	}
	m.input.SetValue("a name being typed")
	before := m.view.at
	m.Update(msg)
	if m.view.at != before-wheelStep {
		t.Errorf("a wheel report over an open list did not scroll the session: %d, want %d", m.view.at, before-wheelStep)
	}
	if got := m.input.Value(); got != "a name being typed" {
		t.Errorf("the wheel wrote %q into the box the list is reading", got)
	}
}

// the chord that used to hand over the mouse is free again: the screen has nothing to hand over
func TestCtrlTIsNotBoundToThePointer(t *testing.T) {
	m := longSession(t)
	m.input.SetValue("x")
	before := m.view.at
	_, cmd := m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if name := strings.ToLower(fmt.Sprintf("%T", msg)); strings.Contains(name, "mouse") {
				t.Errorf("ctrl+t still asks the terminal for the pointer: %s", name)
			}
		}
	}
	if m.view.at != before {
		t.Error("ctrl+t moved the transcript")
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
		if h := lipgloss.Height(m.render()); h != 24 {
			t.Errorf("at width %d the screen is %d rows, want 24", width, h)
		}
		for i, line := range strings.Split(m.render(), "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("at width %d, row %d is %d wide", width, i, w)
			}
		}
	}
}
