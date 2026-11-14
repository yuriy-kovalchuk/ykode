package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/audit"
)

func (m *model) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())

	if text == "" {
		return m, nil
	}
	m.input.Reset()
	m.history = append(m.history, text)
	m.histIdx, m.unsent = len(m.history), ""

	if strings.HasPrefix(text, "/") {
		name, arg, _ := strings.Cut(text, " ")
		if name == "/compact" && !m.running {
			return m, m.compact()
		}
		if name == "/settings" {
			// the screen answers this one: it writes the settings file, and a bare /settings opens
			// the server and model screen, the one a first run starts with
			if strings.TrimSpace(text) == "/settings" {
				m.openSettings()
			} else {
				m.log.output(m.settings(text))
			}
			m.refresh()
			return m, nil
		}
		if name == "/sessions" {
			// the screen answers this one too: its list can put a saved conversation in this run's
			// place, which a front end without a screen leaves to --continue. It would replace the
			// history a request is in the middle of sending, so a running turn waits.
			if m.running {
				m.log.notice("that waits for the turn to finish: press esc to stop this one first")
				m.refresh()
				return m, nil
			}
			m.openSessions()
			m.refresh()
			return m, nil
		}
		if name == "/ctx" || name == "/usage" {
			// the screen reads these itself: figures in a box the terminal cannot scroll away, with the
			// window's fullness shown as a bar instead of a paragraph of them
			m.picker = m.sessionReport()
			m.refresh()
			return m, nil
		}
		if m.running && !agent.CommandIsSafeDuringATurn(text) {
			m.log.notice("that waits for the turn to finish: press esc to stop the turn first")
			m.refresh()
			return m, nil
		}
		if m.running && (name == "/exit" || name == "/quit") {
			m.cancelRun() // leaving cancels the turn rather than racing it
		}
		if name == "/help" {
			// the commands, narrowable, with enter writing one of them in the prompt box
			m.openHelp()
			m.refresh()
			return m, nil
		}
		if name == "/tools" {
			// the tools the model may call, as a list that can be narrowed to one
			m.openTools()
			m.refresh()
			return m, nil
		}
		if name == "/history" {
			// the calls this run made, narrowed by the same words the command takes (failed, outside, a tool
			// name, --all), and a call named by id opens already read in full: what a call is - its arguments,
			// the files it touched, its diff - belongs to the screen, never to the transcript
			all, id := strings.Contains(arg, "--all"), audit.CallID(arg)
			words := strings.TrimSpace(strings.ReplaceAll(arg, "--all", ""))
			m.openHistory(all, words, id)
			m.refresh()
			return m, nil
		}
		if name == "/think" && strings.TrimSpace(text) == "/think" {
			// how deeply the model thinks is a knob on the settings screen, which lists the levels
			m.openSettingsOn("thinking_level")
			m.refresh()
			return m, nil
		}
		if name == "/reset" {
			// one keystroke should not throw the conversation away
			m.confirmCommand(text, "reset", "forget the conversation? the model starts over with only its instructions")
			m.refresh()
			return m, nil
		}
		if name == "/undo" && strings.TrimSpace(arg) == "force" && m.agent.HasUndo() {
			// force writes bytes back over whatever is in the file now, which is worth one question
			m.confirmCommand(text, "undo", "write that file back over what is there now? force does it even when someone changed it since")
			m.refresh()
			return m, nil
		}
		if strings.TrimSpace(text) == "/model" {
			// the screen lists what the server offers and switches to the model you pick; /model ID and
			// /model NUMBER still answer in the transcript, for a model the server does not list
			cmd := m.openModels()
			m.refresh()
			return m, cmd
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
		// the ask goes to the run, which takes it at the next round-trip; the rows under the prompt
		// keep it in sight until then, so it never scrolls away
		m.agent.Steer(text)
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
	m.noAnswer() // a question the run is waiting on is answered by nothing, not left hanging
	if m.cancel == nil {
		return // nothing is running: a second press has nothing to stop and nothing to say
	}
	m.log.notice("cancelling: the turn stops after the tool that is running")
	m.cancel()
	m.cancel = nil
}

func (m *model) handle(ev agent.Event) {
	m.bottom = "" // an event changes what sits under the session, so it is rebuilt

	switch ev.Kind {
	case agent.Thinking, agent.Answer:
		// what streams joins the session now and is drawn with the next frame: a frame per token is
		// more frames than a reader sees
		if ev.Kind == agent.Thinking {
			m.log.thinking(ev.Text)
		} else {
			m.log.answer(ev.Text)
		}
		m.undrawn = true
		return
	case agent.TurnEnd:
		m.decode, m.prefill, m.estimated = ev.Stats.DecodeTokSec, ev.Stats.PrefillTokSec, ev.Stats.Estimated
		m.log.dropDrafts()
	case agent.ToolDraft:
		m.log.draft(ev.Call, ev.Tool, ev.Text)
		m.undrawn = true // the rows of a call being written wait for the next frame, as text does
		return
	case agent.Progress:
		// the first token reports no decode speed yet; keep the last one rather than show 0
		if ev.Stats.DecodeTokSec > 0 {
			m.decode, m.estimated = ev.Stats.DecodeTokSec, ev.Stats.Estimated
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
		m.log.changed(ev.Tool, ev.Args, ev.Text, ev.Change)
	case agent.ToolFail:
		m.log.failed(ev.Tool, ev.Args, ev.Text)
	case agent.Summary:
		m.log.summary(ev.Text) // what compaction folded away stays readable where it happened
	case agent.Notice:
		m.log.notice(ev.Text)
	case agent.Cleared:
		// what the server was doing describes the conversation that went, not the empty one
		m.decode, m.prefill, m.took, m.estimated = 0, 0, 0, false
	case agent.Steered:
		m.log.user(ev.Text, time.Now()) // the run took it: the ask joins where the model reads it
	}
	m.refresh()
}
