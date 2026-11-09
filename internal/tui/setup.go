package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// ErrSetupCancelled is the user leaving the setup screen without choosing a server.
var ErrSetupCancelled = errors.New("setup cancelled: nothing was saved")

// SetupStart is what the setup screen opens with.
type SetupStart struct {
	BaseURL string // prefilled; the built-in default on a first run
	APIKey  string // a key already configured, used for probing when the field is left empty
	Problem string // why the screen is up when it is not a first run, e.g. the server did not answer
	Model   string // the model in use, marked in the list when the server has it
	Inside  bool   // opened over a running session: esc closes it rather than quits
}

// SetupResult is the server the user picked, and what to save.
type SetupResult struct {
	BaseURL string
	APIKey  string // only what was typed; empty keeps what was configured
	Model   string
	CtxSize int
}

// Setup asks which server to talk to, checks it answers, and lets the user pick a model.
func Setup(ctx context.Context, start SetupStart) (SetupResult, error) {
	m := newSetup(start, probeServer)
	final, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	if err != nil {
		return SetupResult{}, err
	}
	s := final.(*setupModel)
	if !s.done {
		return SetupResult{}, ErrSetupCancelled
	}
	return s.result, nil
}

type setupStep int

const (
	stepServer setupStep = iota
	stepProbing
	stepModel
)

type setupModel struct {
	start  SetupStart
	probe  probeFunc
	step   setupStep
	url    textinput.Model
	key    textinput.Model
	focus  int // 0 the URL, 1 the key
	models []llm.ModelInfo
	pick   int
	found  string // the URL that answered, which may have gained /v1
	err    string
	width  int
	done   bool // a server and model were chosen
	closed bool // the screen is finished with, chosen or not; a host reads this instead of waiting for the quit
	result SetupResult
}

func newSetup(start SetupStart, probe probeFunc) *setupModel {
	url := textinput.New()
	url.Prompt = ""
	url.Placeholder = "http://127.0.0.1:8080/v1"
	url.SetValue(start.BaseURL)
	url.CharLimit = 300
	url.Width = 56
	url.Focus()

	key := textinput.New()
	key.Prompt = ""
	key.Placeholder = "none"
	if start.APIKey != "" {
		key.Placeholder = "leave empty to keep the one configured"
	}
	key.EchoMode = textinput.EchoPassword
	key.CharLimit = 500
	key.Width = 56

	return &setupModel{start: start, probe: probe, url: url, key: key, err: start.Problem, width: defaultWidth}
}

func (m *setupModel) Init() tea.Cmd { return textinput.Blink }

func (m *setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case probeDoneMsg:
		if msg.err != nil {
			m.step, m.err = stepServer, fmt.Sprintf("%s did not answer: %v", strings.TrimSpace(m.url.Value()), msg.err)
			return m, nil
		}
		m.step, m.err, m.found, m.models, m.pick = stepModel, "", msg.baseURL, msg.models, 0
		for i, model := range msg.models {
			if model.ID == m.start.Model {
				m.pick = i // open on the model in use, so enter alone keeps it
			}
		}
		return m, nil
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m, nil
}

// onKey handles a key press for the step on screen.
func (m *setupModel) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.closed = true
		return m, tea.Quit
	}
	switch m.step {
	case stepProbing:
		return m, nil // the probe reports back on its own; a timeout ends it
	case stepModel:
		switch msg.String() {
		case "up", "k":
			m.pick = (m.pick + len(m.models) - 1) % len(m.models)
		case "down", "j":
			m.pick = (m.pick + 1) % len(m.models)
		case "esc":
			m.step = stepServer
		case "enter":
			chosen := m.models[m.pick]
			m.done, m.closed = true, true
			m.result = SetupResult{BaseURL: m.found, APIKey: strings.TrimSpace(m.key.Value()), Model: chosen.ID, CtxSize: chosen.CtxSize}
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg.String() {
	case "esc":
		m.closed = true
		return m, tea.Quit
	case "tab", "shift+tab", "up", "down":
		m.focus = 1 - m.focus
		if m.focus == 0 {
			m.key.Blur()
			return m, m.url.Focus()
		}
		m.url.Blur()
		return m, m.key.Focus()
	case "enter":
		url := strings.TrimSuffix(strings.TrimSpace(m.url.Value()), "/")
		if url == "" {
			m.err = "the server's URL is needed, e.g. http://127.0.0.1:8080/v1"
			return m, nil
		}
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			url = "http://" + url
		}
		m.url.SetValue(url)
		key := strings.TrimSpace(m.key.Value())
		if key == "" {
			key = m.start.APIKey
		}
		m.step, m.err = stepProbing, ""
		probe := m.probe
		return m, func() tea.Msg {
			found, models, err := probe(context.Background(), url, key)
			return probeDoneMsg{baseURL: found, models: models, err: err}
		}
	}
	var cmd tea.Cmd
	if m.focus == 0 {
		m.url, cmd = m.url.Update(msg)
	} else {
		m.key, cmd = m.key.Update(msg)
	}
	return m, cmd
}

var (
	setupBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("6")).Padding(1, 2)
	setupTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	setupLabel = lipgloss.NewStyle().Bold(true)
	setupDim   = lipgloss.NewStyle().Faint(true)
	setupError = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	setupPick  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
)

func (m *setupModel) View() string {
	var b strings.Builder
	title := "ykode · connect to a model server"
	if m.start.Inside {
		title = "ykode · settings · server and model"
	}
	b.WriteString(setupTitle.Render(title) + "\n\n")
	switch m.step {
	case stepServer, stepProbing:
		b.WriteString(setupDim.Render("ykode talks to an OpenAI-compatible server, such as llama.cpp's\nllama-server (started with --jinja for tool calls).") + "\n\n")
		b.WriteString(field("Server URL", m.url.View(), m.focus == 0) + "\n\n")
		b.WriteString(field("API key (optional)", m.key.View(), m.focus == 1) + "\n\n")
		if m.step == stepProbing {
			b.WriteString(setupDim.Render("asking the server for its models…") + "\n\n")
		} else if m.err != "" {
			b.WriteString(setupError.Render(strings.Join(wrap(m.err, 60), "\n")) + "\n\n")
		}
		leave := "esc quits"
		if m.start.Inside {
			leave = "esc closes"
		}
		b.WriteString(setupDim.Render("enter connects · tab switches field · " + leave))
	case stepModel:
		b.WriteString(fmt.Sprintf("%s answered with %d model(s). Which one should ykode use?\n\n", m.found, len(m.models)))
		for i, model := range m.models {
			line := model.ID
			if model.CtxSize > 0 {
				line += setupDim.Render(fmt.Sprintf("  %s context", tokCount(model.CtxSize)))
			}
			if model.ID == m.start.Model {
				line += setupDim.Render("  in use")
			}
			if i == m.pick {
				b.WriteString(setupPick.Render("❯ ") + setupPick.Render(model.ID) + strings.TrimPrefix(line, model.ID) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
		b.WriteString("\n" + setupDim.Render("enter saves it to settings.yaml and switches to it · ↑/↓ picks · esc goes back"))
	}
	return setupBox.Render(b.String())
}

// field is one labelled input; the focused one is marked.
func field(label, input string, focused bool) string {
	mark := "  "
	if focused {
		mark = setupPick.Render("❯ ")
	}
	return setupLabel.Render(label) + "\n" + mark + input
}
