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
