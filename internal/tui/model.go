package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
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
	root     string // the project the footer names
	branch   string // what HEAD points at there, read again with each prompt
	input    textarea.Model
	view     viewport.Model
	log      transcript
	decode   float64 // tokens per second of the last answer
	prefill  float64 // tokens per second the last prompt was read at
	started  time.Time
	took     time.Duration // how long the last prompt took
	spin     spinner.Model
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
	mouseOff bool   // the terminal has the mouse back, so text can be selected and copied
	showKeys bool   // ? lists the keys above the prompt
	unseen   bool   // output arrived below while the reader was scrolled up
	rendered string // the transcript as last drawn, to tell new output from a redraw
}

func newModel(ctx context.Context, a *agent.Agent, hub *Hub) *model {
	input := textarea.New()
	input.Placeholder = "ask for a change, /help lists the commands"
	input.Prompt = ""
	input.ShowLineNumbers = false
	input.SetHeight(1)
	input.Focus() // an unfocused box swallows every keystroke, so nothing can be typed
	// Enter sends, so a newline has to be asked for. ctrl+j works everywhere; alt+enter and
	// shift+enter work when the terminal reports the modifiers at all.
	input.KeyMap.InsertNewline.SetKeys("ctrl+j", "alt+enter", "shift+enter")

	spin := spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(styleWorking))

	// a terminal that will not say its size still gets a screen
	return &model{
		spin:   spin,
		ctx:    ctx,
		hub:    hub,
		agent:  a,
		root:   a.Root(),
		branch: agent.GitBranch(a.Root()),
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
		m.input.SetWidth(m.width - lipgloss.Width(promptMark))
		m.refresh()
		return m, nil
	case tea.MouseMsg:
		m.wheel(msg)
		return m, nil
	case eventMsg:
		m.handle(msg.ev)
		return m, nil
	case confirmMsg:
		// parallel calls can ask at once: the first asks, the rest wait their turn
		m.pending = append(m.pending, &question{text: msg.ev.Text, target: msg.ev.Target, reply: msg.reply})
		m.refresh()
		return m, nil
	case spinner.TickMsg:
		if !m.running {
			return m, nil // the spinner stops with the run, and so does the timer it redraws
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case compactDoneMsg:
		m.running = false
		m.took = time.Since(m.started)
		m.cancel = nil
		m.log.notice(strings.TrimSuffix(agent.CompactReport(msg.freed), "\n"))
		m.refresh()
		return m, nil
	case runDoneMsg:
		m.running = false
		m.took = time.Since(m.started)
		m.log.done(time.Now(), m.took)
		m.cancel = nil
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		var cmd tea.Cmd
		if len(m.queue) > 0 {
			cmd = m.start(m.queue[0])
			m.queue = m.queue[1:]
		}
		m.refresh()
		return m, cmd
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
	case "ctrl+t":
		// a captured mouse cannot select text, so the user can hand it back to the terminal
		m.mouseOff = !m.mouseOff
		m.refresh()
		if m.mouseOff {
			return m, tea.DisableMouse
		}
		return m, tea.EnableMouseCellMotion
	case "pgup":
		m.view.PageUp()
		return m, nil
	case "pgdown":
		m.view.PageDown()
		m.seenIfAtBottom()
		return m, nil
	case "ctrl+u":
		m.view.HalfPageUp()
		return m, nil
	case "ctrl+d":
		m.view.HalfPageDown()
		m.seenIfAtBottom()
		return m, nil
	case "enter":
		return m.submit()
	}

	// with an empty box the arrows walk the prompts you sent; with text they move the cursor
	if m.input.Value() == "" {
		switch msg.String() {
		case "?":
			m.showKeys = !m.showKeys
			m.refresh()
			return m, nil
		case "end":
			m.view.GotoBottom()
			m.unseen = false
			return m, nil
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
	m.fitInput()
	m.layout()
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

// refresh keeps a reader who scrolled up in place and marks the output arriving below.
func (m *model) refresh() {
	atBottom := m.view.AtBottom()
	m.layout()
	content := m.log.render(m.width, m.expanded)
	if content != m.rendered && !atBottom && m.rendered != "" {
		m.unseen = true
	}
	m.rendered = content
	m.view.SetContent(content)
	if atBottom {
		m.view.GotoBottom()
		m.unseen = false
	}
}

// layout shrinks the transcript, because the question, the keys and the input can all grow.
func (m *model) layout() {
	m.view.Height = max(m.height-lipgloss.Height(m.bottomArea()), 3)
}

func (m *model) View() string {
	return lipgloss.JoinVertical(lipgloss.Left, m.view.View(), m.bottomArea())
}

func rule(width int) string {
	return "\x1b[2m" + strings.Repeat("─", max(width, 0)) + "\x1b[0m"
}

// cutToWidth keeps a plain line inside the screen, ending in … when something was cut.
func cutToWidth(s string, width int) string {
	if width <= 1 || lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > width {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// status is what the footer reports right now.
func (m *model) status() status {
	s := status{root: m.root, branch: m.branch, model: m.agent.Model(), thinking: m.agent.ThinkingLevel(),
		usage: m.agent.Usage(), ctxSize: m.agent.CtxSize(), prefill: m.prefill,
		decode: m.decode, running: m.running, queued: len(m.queue), took: m.took, spinner: m.spin.View(),
		keysHint: !m.showKeys, width: m.width}
	if m.running {
		s.elapsed = time.Since(m.started)
	}
	return s
}
