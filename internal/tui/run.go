package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

func (m *model) submit() (tea.Model, tea.Cmd) {
	shown := strings.TrimSpace(m.input.Value())

	if shown == "" {
		return m, nil
	}
	m.input.Reset()
	// history keeps the tokens, which still stand for their pastes when an old ask is sent again
	m.history = append(m.history, shown)
	m.histIdx, m.unsent = len(m.history), ""
	text := m.pastes.expand(shown)

	if strings.HasPrefix(text, "/") {
		name, _, _ := strings.Cut(text, " ")
		if m.running && (name == "/exit" || name == "/quit") {
			m.cancelRun() // leaving cancels the turn rather than racing it
		}
		// one owner for the waiting rule: the table says which commands wait, and the gate runs first
		if m.running && !agent.CommandIsSafeDuringATurn(text) {
			m.log.notice("that waits for the turn to finish: press esc to stop this one first")
			m.refresh()
			return m, nil
		}
		if name == "/compact" {
			// the gate above holds /compact, since the fold replaces the turns a request is built from
			return m, m.compact()
		}
		if name == "/create-local-setting" {
			// the project gets its own settings file here, and every change after this lands in it
			m.createLocal()
			m.refresh()
			return m, nil
		}
		if name == "/settings" {
			// the screen answers this: it writes the settings file, and a bare /settings opens the first screen
			if strings.TrimSpace(text) == "/settings" {
				m.openSettings()
			} else {
				m.log.output(m.settings(text))
			}
			m.refresh()
			return m, nil
		}
		if name == "/sessions" {
			// the screen answers this too: its list swaps in a saved conversation, so the gate above holds it
			m.openSessions()
			m.refresh()
			return m, nil
		}
		if name == "/ctx" || name == "/usage" {
			// the screen reads these itself: figures in a box, the window's fullness shown as a bar
			m.picker = m.sessionReport()
			m.refresh()
			return m, nil
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
		if strings.TrimSpace(text) == "/model" {
			// the screen lists the server's models; /model ID and NUMBER still answer in the transcript
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
		// the ask goes to the run for the next round-trip, staying in sight under the prompt until then
		m.agent.Steer(text)
		m.refresh()
		return m, nil
	}
	return m, m.startShown(shown, text)
}

// start runs one turn on its own goroutine, which always reports back, good or bad
func (m *model) start(prompt string) tea.Cmd { return m.startShown(prompt, prompt) }

// startShown is start with the ask's card showing a paste as its token, not as every line of it
func (m *model) startShown(shown, prompt string) tea.Cmd {
	m.log.user(shown, time.Now())
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

// compact folds on its own goroutine: the summary is the slowest thing here, and the screen runs on
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
		// what streams joins the session now and draws with the next frame: a frame per token is wasted
		if ev.Kind == agent.Thinking {
			m.log.thinking(ev.Text)
		} else {
			m.log.answer(ev.Text)
		}
		m.undrawn = true
		return
	case agent.TurnEnd:
		m.decode, m.prefill, m.estimated = ev.Stats.DecodeTokSec, ev.Stats.PrefillTokSec, ev.Stats.Estimated
		m.read, m.readTotal = 0, 0
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
		if ev.Stats.ReadTotal > 0 {
			m.read, m.readTotal = ev.Stats.ReadTokens, ev.Stats.ReadTotal
		}
		// a timings chunk moves only the footer, built from what is here now, so no transcript redraw
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
