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
	ctx       context.Context
	hub       *Hub // what a run reports to, including that it finished
	agent     *agent.Agent
	root      string    // the project the footer names
	branch    string    // what HEAD points at there, read again with each prompt
	head      headWatch // where that project's HEAD file is, and how it looked when last seen
	input     textarea.Model
	view      window
	log       transcript
	decode    float64 // tokens per second of the last answer
	prefill   float64 // tokens per second the last prompt was read at
	estimated bool    // the speeds above were measured here while the reply streamed, not reported by the server
	started   time.Time
	took      time.Duration // how long the last prompt took
	spin      spinner.Model
	width     int
	height    int
	running   bool
	cancel    context.CancelFunc
	pending   []*question // questions waiting for an answer, oldest first
	err       string
	// suggest is what the typed line could still become, and suggestFor the line that list was
	// made from: a different line brings a cleared list back.
	suggest    []agent.CommandSpec
	suggestFor string
	cfg        config.Config // what this run reads, which /settings shows and changes
	history    []string
	histIdx    int
	unsent     string // what is in the box while the earlier prompts are being read
	mouseOn    bool   // the screen has the mouse: the wheel scrolls the transcript, and the terminal cannot select
	planFolded bool   // ctrl+o folds the plan to its heading
	showKeys   bool   // ? lists the keys above the prompt
	unseen     bool   // output arrived below while the reader was scrolled up
	bottom     string // what sits under the session this message; "" until someone asks for it

	// What streams is drawn at the pace of frames, not of tokens. undrawn says the session holds
	// text the screen has not drawn, drawn is when it last drew, and frameDue says a frame is already
	// on its way to draw what is waiting.
	undrawn  bool
	drawn    time.Time
	frameDue bool

	probe     probeFunc       // how the modal asks a server for its models; tests put a fake here
	screen    *settingsScreen // the list of every setting /settings opens over the session; nil when closed
	wantMouse bool            // a setting turned the mouse on or off: the terminal is told on the next update
	modal     *setupModel     // the server and model screen /settings opens over the session; nil when closed
}

func newModel(ctx context.Context, a *agent.Agent, hub *Hub, cfg config.Config) *model {
	input := textarea.New()
	input.Placeholder = "one sentence is enough, /help lists the commands"
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
		head:    watchHead(a.Root()),
		input:   input,
		probe:   probeServer,
		view:    window{height: defaultHeight},
		width:   defaultWidth,
		height:  defaultHeight,
	}
	m.log.root = a.Root() // a call names its file from where the project starts
	for _, note := range cfg.Notices {
		m.log.notice(note) // what startup decided for the user is the first thing on the screen
	}
	m.history = append(m.history, resume(&m.log, cfg.Session, a.Conversation())...)
	m.histIdx = len(m.history)
	if len(m.history) > 0 {
		m.refresh() // a carried-over conversation is on the screen before the first keystroke
	}
	return m
}

func (m *model) Init() tea.Cmd { return tea.Batch(textarea.Blink, m.watchBranch()) }

// frameInterval is the least time between two draws of text that is streaming: thirty frames a
// second reads as text being written, and more than that is laid out to be thrown away.
const frameInterval = time.Second / 30

// frameMsg is a frame that was put off arriving.
type frameMsg struct{}

// paint draws what streamed since the last frame, or puts it off until the next one is due. The
// first piece of a reply is drawn as it arrives: nothing was drawn for a while, so nothing waits.
func (m *model) paint() tea.Cmd {
	if !m.undrawn {
		return nil
	}
	wait := frameInterval - time.Since(m.drawn)
	if wait <= 0 {
		m.refresh()
		return nil
	}
	if m.frameDue {
		return nil
	}
	m.frameDue = true
	return tea.Tick(wait, func(time.Time) tea.Msg { return frameMsg{} })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.bottom = "" // the screen under the session is rebuilt for this message, whoever asks first
	if _, woken := msg.(wakeMsg); woken {
		// what the run reported since the last look, each message handled as if it had come alone
		var cmds []tea.Cmd
		for _, queued := range m.hub.take() {
			_, cmd := m.Update(queued)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
	if m.modal != nil {
		if cmd, taken := m.modalMsg(msg); taken {
			return m, cmd
		}
	} else if m.screen != nil {
		if cmd, taken := m.screenMsg(msg); taken {
			return m, tea.Batch(cmd, m.mouseCmd())
		}
	}
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
		return m, m.paint()
	case frameMsg:
		m.frameDue = false
		if m.undrawn {
			m.refresh()
		}
		return m, nil
	case branchTickMsg:
		m.checkBranch()
		return m, m.watchBranch()
	case confirmMsg:
		// parallel calls can ask at once: the first asks, the rest wait their turn
		m.pending = append(m.pending, &question{text: msg.ev.Text, tool: msg.ev.Tool, args: msg.ev.Args, target: msg.ev.Target, reply: msg.reply})
		m.refresh()
		return m, nil
	case askMsg:
		// the model asked in words; the box under the bar is where they come back
		m.pending = append(m.pending, &question{text: msg.ev.Text, options: msg.ev.Options, words: msg.reply})
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
