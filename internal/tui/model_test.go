package tui

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

func newTestModel() *model {
	a := agent.New(nil, nil, "test-model", agent.Options{}, nil)
	return newModel(context.Background(), a, NewHub(), config.Config{})
}

func TestAScreenThatWasNeverResizedStillDraws(t *testing.T) {
	m := newTestModel()
	m.handle(agent.Event{Kind: agent.Answer, Text: "the answer"})
	m.Update(spinner.TickMsg{}) // what streamed is drawn on the tick
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

// aLongSession is a session the length of a working afternoon: the frame it costs is what the
// screen pays for every few tokens.
func aLongSession(calls int) transcript {
	var log transcript
	for i := 0; i < calls; i++ {
		log.user("a prompt that asks for something reasonable in this project", time.Now())
		log.toolStart("edit_file", `{"path":"internal/tui/rows.go","edits":[{"oldText":"x","newText":"y"}]}`)
		log.toolEnd("edit_file", `{"path":"internal/tui/rows.go"}`, editResult)
	}
	log.answer("the answer the model wrote, with **shape** in it")
	return log
}

// the frame the screen pays must not grow with the session: only the rows in view are drawn, and
// only the block that changed is drawn again.
func BenchmarkAFrameOfALongSession(b *testing.B) {
	for _, calls := range []int{50, 300, 1200} {
		a := agent.New(nil, nil, "test-model", agent.Options{}, nil)
		m := newModel(context.Background(), a, NewHub(), config.Config{})
		m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
		m.log = aLongSession(calls)
		m.refresh() // the session is drawn once; what follows is what a streamed token costs
		b.Run(strconv.Itoa(calls)+" calls", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				m.bottom = "" // what every message does: what sits under the session is rebuilt
				m.refresh()
				_ = m.View()
			}
		})
	}
}
