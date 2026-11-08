package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
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
	view     window
	log      transcript
	decode   float64 // tokens per second of the last answer
	prefill  float64 // tokens per second the last prompt was read at
	started  time.Time
	took     time.Duration // how long the last prompt took
	spin     spinner.Model
	width    int
	height   int
	running  bool
	cancel   context.CancelFunc
	pending  []*question // questions waiting for an answer, oldest first
	err      string
	expanded bool // tool rows also show what they were asked to do
	// suggest is what the typed line could still become, and suggestFor the line that list was
	// made from: a different line brings a cleared list back.
	suggest    []agent.CommandSpec
	suggestFor string
	cfg        config.Config // what this run reads, which /settings shows and changes
	history    []string
	histIdx    int
	unsent     string // what is in the box while the earlier prompts are being read
	mouseOn    bool   // the screen has the mouse: the wheel scrolls the transcript, and the terminal cannot select
	showKeys   bool   // ? lists the keys above the prompt
	unseen     bool   // output arrived below while the reader was scrolled up

	// redrawQueued is a call being written waiting for the spinner's next tick to be drawn: every
	// token of it would otherwise redraw the screen, and the stream waits for that draw.
	redrawQueued bool
}

func newModel(ctx context.Context, a *agent.Agent, hub *Hub, cfg config.Config) *model {
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
	m := &model{
		spin:    spin,
		ctx:     ctx,
		hub:     hub,
		agent:   a,
		cfg:     cfg,
		mouseOn: cfg.Mouse,
		root:    a.Root(),
		branch:  a.Branch(), // the branch the prompt was built on; the next ask refreshes it
		input:   input,
		view:    window{height: defaultHeight},
		width:   defaultWidth,
		height:  defaultHeight,
	}
	m.history = append(m.history, resume(&m.log, cfg.Session, a.Conversation())...)
	m.histIdx = len(m.history)
	if len(m.history) > 0 {
		m.refresh() // a carried-over conversation is on the screen before the first keystroke
	}
	return m
}

func (m *model) Init() tea.Cmd { return textarea.Blink }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
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
		if m.redrawQueued {
			m.redrawQueued = false
			m.refresh() // what streamed since the last tick is drawn here, once
		}
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
		if ask, ok := m.agent.NextWaiting(); ok {
			cmd = m.start(ask) // an ask that never reached a round-trip starts as an ordinary prompt
		}
		m.refresh()
		return m, cmd
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}
