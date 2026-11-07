package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// Messages the screen receives from the run it started.
type eventMsg struct{ ev agent.Event }
type confirmMsg struct {
	ev    agent.Event
	reply chan agent.Decision
}
type runDoneMsg struct{ err error }
type compactDoneMsg struct{ freed int } // the summary the model wrote for /compact

// question is a confirmation waiting for an answer; the run is blocked until it gets one.
type question struct {
	text   string
	target []string
	reply  chan agent.Decision
}

// defaultSize is what the screen assumes until the terminal reports its own.
const (
	defaultWidth  = 80
	defaultHeight = 24
)

type model struct {
	ctx      context.Context
	hub      *Hub // what a run reports to, including that it finished
	agent    *agent.Agent
	input    textarea.Model
	view     viewport.Model
	log      transcript
	usage    agent.Usage
	decode   float64 // tokens per second of the last answer
	width    int
	height   int
	running  bool
	queue    []string // what was typed while the run was busy
	cancel   context.CancelFunc
	pending  []*question // questions waiting for an answer, oldest first
	err      string
	expanded bool // tool rows also show what they were asked to do
	history  []string
	histIdx  int
}

func newModel(ctx context.Context, a *agent.Agent, hub *Hub) *model {
	input := textarea.New()
	input.Placeholder = "ask for a change, /help lists the commands"
	input.Prompt = ""
	input.ShowLineNumbers = false
	input.SetHeight(2)
	input.Focus() // an unfocused box swallows every keystroke, so nothing can be typed
	// Enter sends, so a newline has to be asked for: ctrl+j always works.
	input.KeyMap.InsertNewline.SetKeys("ctrl+j", "shift+enter")

	// a terminal that will not say its size still gets a screen
	return &model{
		ctx:    ctx,
		hub:    hub,
		agent:  a,
		input:  input,
		view:   viewport.New(defaultWidth, defaultHeight),
		width:  defaultWidth,
		height: defaultHeight,
	}
}

func (m *model) Init() tea.Cmd { return textarea.Blink }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.view.Width = msg.Width
		m.view.Height = m.logHeight()
		m.input.SetWidth(m.width - 2)
		m.refresh()
		return m, nil
	case eventMsg:
		m.handle(msg.ev)
		return m, nil
	case confirmMsg:
		// parallel calls can ask at once: the first asks, the rest wait their turn
		m.pending = append(m.pending, &question{text: msg.ev.Text, target: msg.ev.Target, reply: msg.reply})
		m.refresh()
		return m, nil
	case compactDoneMsg:
		m.running = false
		m.cancel = nil
		m.log.notice(strings.TrimSuffix(agent.CompactReport(msg.freed), "\n"))
		m.refresh()
		return m, nil
	case runDoneMsg:
		m.running = false
		m.cancel = nil
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		if len(m.queue) > 0 {
			m.start(m.queue[0])
			m.queue = m.queue[1:]
		}
		m.refresh()
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.pending) > 0 {
		switch msg.String() {
		case "y", "Y":
			m.reply(agent.Allow)
		case "a", "A":
			m.reply(agent.AllowAlways)
		case "n", "N", "esc", "enter", "ctrl+c":
			m.reply(agent.Refuse)
		}
		m.refresh()
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		if m.running {
			m.cancelRun()
			return m, nil
		}
		return m, tea.Quit
	case "esc":
		if m.running {
			m.cancelRun()
			return m, nil
		}
	case "ctrl+o":
		m.expanded = !m.expanded
		m.refresh()
		return m, nil
	case "pgup", "ctrl+u":
		m.view.PageUp()
		return m, nil
	case "pgdown", "ctrl+d":
		m.view.PageDown()
		return m, nil
	case "enter":
		return m.submit()
	}

	// with an empty box the arrows walk the prompts you sent; with text they move the cursor
	if m.input.Value() == "" {
		switch msg.String() {
		case "up":
			if m.histIdx > 0 {
				m.histIdx--
				m.input.SetValue(m.history[m.histIdx])
			}
			return m, nil
		case "down":
			if m.histIdx < len(m.history)-1 {
				m.histIdx++
				m.input.SetValue(m.history[m.histIdx])
			} else {
				m.histIdx = len(m.history)
				m.input.SetValue("")
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// reply answers the question on the screen; the run that asked it is blocked until then.
func (m *model) reply(d agent.Decision) {
	if len(m.pending) == 0 {
		return
	}
	m.pending[0].reply <- d
	m.pending = m.pending[1:]
}

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
			m.compact()
			return m, nil
		}
		if m.running && !agent.CommandIsSafeDuringATurn(text) {
			m.log.notice("that waits for the turn to finish: press esc to stop the turn first")
			m.refresh()
			return m, nil
		}
		if m.running && strings.HasPrefix(text, "/ex") {
			m.cancelRun() // leaving cancels the turn rather than racing it
		}
		out, quit, err := m.agent.Command(text)
		if line := strings.TrimRight(out, "\n"); line != "" {
			m.log.notice(line)
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
	m.start(text)
	return m, nil
}

// start runs one turn on its own goroutine: the screen keeps drawing and typing. The
// goroutine always reports back, good or bad, or the screen would wait forever.
func (m *model) start(prompt string) {
	m.log.user(prompt)
	turnCtx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.running = true
	m.refresh()
	go func() {
		m.hub.post(runDoneMsg{err: m.agent.Ask(turnCtx, prompt)})
	}()
}

// compact folds the older turns on its own goroutine: the model writes the summary, which is
// the slowest thing in a session, and the screen must keep drawing and answering questions.
func (m *model) compact() {
	turnCtx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.running = true
	m.log.notice("compacting: the model is writing a summary of the older turns")
	m.refresh()
	go func() {
		m.hub.post(compactDoneMsg{freed: m.agent.Compact(turnCtx)})
	}()
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
		m.usage = m.agent.Usage()
		m.decode = ev.Stats.DecodeTokSec
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

// refresh redraws the transcript, staying at the bottom unless the reader scrolled up.
func (m *model) refresh() {
	atBottom := m.view.AtBottom()
	m.view.SetContent(m.log.render(m.width, m.expanded))
	if atBottom {
		m.view.GotoBottom()
	}
}

func (m *model) View() string {
	status := statusLine(m.agent.Model(), m.usage, m.agent.CtxSize(), m.decode, m.running, len(m.queue))
	return lipgloss.JoinVertical(lipgloss.Left,
		m.view.View(),
		m.statusArea(status),
		m.bottomArea(),
	)
}

func (m *model) statusArea(status string) string {
	line := rule(m.width) + "\n" + status
	if m.err != "" {
		line += "\n\x1b[31m" + oneLine(m.err, m.width) + "\x1b[0m"
	}
	return line
}

func (m *model) bottomArea() string {
	if len(m.pending) > 0 {
		q := m.pending[0]
		targets := ""
		if len(q.target) > 0 {
			targets = "\n  \x1b[33moutside the project: " + strings.Join(q.target, ", ") + "\x1b[0m"
		}
		more := ""
		if len(m.pending) > 1 {
			more = fmt.Sprintf(" \x1b[2m(%d more waiting)\x1b[0m", len(m.pending)-1)
		}
		return "⚠ " + q.text + " \x1b[1m[y/N/a]\x1b[0m" + targets + more + "\n" + rule(m.width)
	}
	return "❯ " + m.input.View() + "\n" + "\x1b[2menter sends · ctrl+j newline · esc stops · ctrl+o tool rows · ctrl+c quits\x1b[0m"
}

// logHeight is what is left for the transcript once the status and the input are placed.
func (m *model) logHeight() int {
	h := m.height - m.input.Height() - 4
	if h < 3 {
		h = 3
	}
	return h
}

func rule(width int) string {
	return "\x1b[2m" + strings.Repeat("─", max(width, 0)) + "\x1b[0m"
}
