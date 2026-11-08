package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

func newTestModel() *model {
	a := agent.New(nil, nil, "test-model", agent.Options{}, nil)
	return newModel(context.Background(), a, NewHub())
}

func TestAScreenThatWasNeverResizedStillDraws(t *testing.T) {
	m := newTestModel()
	m.handle(agent.Event{Kind: agent.Answer, Text: "the answer"})
	out := stripCodes(m.View())
	if !strings.Contains(out, "the answer") || !strings.Contains(out, "test-model") {
		t.Errorf("an unsized terminal drew nothing useful:\n%s", out)
	}
}

// newWiredModel is a session whose run reports back to a queue the test feeds to Update,
// the way a running program's event loop would.
// wired is a session plus the queue its run reports into, so a test can be the event loop.

// answered says whether a question was replied to, without waiting forever if it was not.

// quits says whether a command ends the program; starting a run returns the spinner's tick instead
func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}
