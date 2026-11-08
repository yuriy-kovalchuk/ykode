package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

func (m *model) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())

	if text == "" {
		return m, nil
	}
	m.input.Reset()
	m.history = append(m.history, text)
	m.histIdx = len(m.history)

	if strings.HasPrefix(text, "/") {
		if strings.HasPrefix(text, "/compact") && !m.running {
			return m, m.compact()
		}
		if m.running && !agent.CommandIsSafeDuringATurn(text) {
			m.log.notice("that waits for the turn to finish: press esc to stop the turn first")
			m.refresh()
			return m, nil
		}
		if m.running && strings.HasPrefix(text, "/ex") {
			m.cancelRun() // leaving cancels the turn rather than racing it
		}
		out, quit, err := m.agent.Command(m.ctx, text)
		if out = strings.TrimRight(out, "\n"); out != "" {
			m.log.output(out)
		}
		if err != nil {
			m.err = err.Error()
		}
		m.refresh()
		if quit {
			return m, tea.Quit
		}
		return m, nil
	}

	if m.running {
		m.queue = append(m.queue, text)
		m.log.notice("queued until this turn ends: " + oneLine(text, 80))
		m.refresh()
		return m, nil
	}
	return m, m.start(text)
}

// start runs one turn on its own goroutine: the screen keeps drawing and typing. The
// goroutine always reports back, good or bad, or the screen would wait forever.
func (m *model) start(prompt string) tea.Cmd {
	m.log.user(prompt, time.Now())
	turnCtx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.begin()
	go func() {
		m.hub.post(runDoneMsg{err: m.agent.Ask(turnCtx, prompt)})
	}()
	return m.spin.Tick
}

// begin marks the screen busy and starts the clock the status line counts.
func (m *model) begin() {
	m.running, m.started, m.took = true, time.Now(), 0
	m.err = ""                         // what failed last turn is not a notice to keep reading next turn
	m.branch = agent.GitBranch(m.root) // the run may have changed branch with a command
	m.refresh()
}

// compact folds the older turns on its own goroutine: the model writes the summary, which is
// the slowest thing in a session, and the screen must keep drawing and answering questions.
func (m *model) compact() tea.Cmd {
	turnCtx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.log.notice("compacting: the model is writing a summary of the older turns")
	m.begin()
	go func() {
		m.hub.post(compactDoneMsg{freed: m.agent.Compact(turnCtx)})
	}()
	return m.spin.Tick
}

func (m *model) cancelRun() {
	m.log.notice("cancelling: the turn stops after the tool that is running")
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *model) handle(ev agent.Event) {
	switch ev.Kind {
	case agent.Thinking:
		m.log.thinking(ev.Text)
	case agent.Answer:
		m.log.answer(ev.Text)
	case agent.TurnEnd:
		m.decode, m.prefill = ev.Stats.DecodeTokSec, ev.Stats.PrefillTokSec
		m.log.dropDrafts()
	case agent.ToolDraft:
		m.log.draft(ev.Call, ev.Tool, ev.Text)
	case agent.Progress:
		// the first token reports no decode speed yet; keep the last one rather than show 0
		if ev.Stats.DecodeTokSec > 0 {
			m.decode = ev.Stats.DecodeTokSec
		}
		if ev.Stats.PrefillTokSec > 0 {
			m.prefill = ev.Stats.PrefillTokSec
		}
		// a timings chunk moves only the footer, and every draw builds the footer from what is
		// here now, so the transcript is not drawn again for it
		return
	case agent.ToolStart:
		m.log.toolStart(ev.Tool, ev.Args)
	case agent.ToolEnd:
		m.log.toolEnd(ev.Tool, ev.Args, ev.Text)
	case agent.ToolFail:
		m.log.failed(ev.Tool, ev.Text)
	case agent.Notice:
		m.log.notice(ev.Text)
	}
	m.refresh()
}
