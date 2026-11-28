package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/theme"
)

// ErrSetupCancelled is the user leaving the setup screen without choosing a server.
var ErrSetupCancelled = errors.New("setup cancelled: nothing was saved")

// SetupStart is what the setup screen opens with.
type SetupStart struct {
	BaseURL string // prefilled; the built-in default on a first run
	Problem string // why the screen is up when it is not a first run, e.g. the server did not answer
	Model   string // the model in use, marked in the list when the server has it
	Inside  bool   // opened over a running session: esc closes it rather than quits
	Theme   string // the theme setting, so the first screen a user sees is already in it
}

// SetupResult is the server the user picked, and what to save.
type SetupResult struct {
	BaseURL string
	Model   string
	CtxSize int
	Vision  bool // the model picked reads pictures
}

// Setup asks which server to talk to, checks it answers, and lets the user pick a model.
func Setup(ctx context.Context, start SetupStart) (SetupResult, error) {
	// the same belt the session wears: no mode this program, or a killed one before it, outlives it
	writeModesOff(os.Stdout)
	defer writeModesOff(os.Stdout)
	m := newSetup(start, probeServer)
	m.altScreen = true
	m.ownPalette()
	final, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
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
	start     SetupStart
	probe     probeFunc
	step      setupStep
	url       textinput.Model
	models    []llm.ModelInfo
	pick      int
	found     string // the URL that answered, which may have gained /v1
	err       string
	width     int
	done      bool          // a server and model were chosen
	closed    bool          // the screen is finished with, chosen or not; a host reads this instead of waiting for the quit
	altScreen bool          // run on its own it takes the whole terminal; over a session it is only a box
	painted   theme.Palette // run on its own, the palette it draws in and paints the terminal with
	result    SetupResult
}

func newSetup(start SetupStart, probe probeFunc) *setupModel {
	url := textinput.New()
	url.Prompt = ""
	url.Placeholder = "http://127.0.0.1:8080/v1"
	url.SetValue(start.BaseURL)
	url.CharLimit = 300
	url.SetWidth(56)
	url.Focus()

	return &setupModel{start: start, probe: probe, url: url, err: start.Problem, width: defaultWidth}
}

// ownPalette is for the screen run on its own: over a session it draws in the session's palette instead
func (m *setupModel) ownPalette() {
	m.painted = paletteFor(m.start.Theme, true)
	usePalette(m.painted)
}

// Init asks for the background as the session does, since auto can only be answered by the terminal
func (m *setupModel) Init() tea.Cmd { return tea.Batch(textinput.Blink, tea.RequestBackgroundColor) }

func (m *setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.BackgroundColorMsg:
		if _, named := theme.Named(m.start.Theme); !named {
			m.painted = theme.For(msg.IsDark())
			usePalette(m.painted)
		}
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
	case tea.PasteMsg:
		if m.step == stepServer {
			var cmd tea.Cmd
			m.url, cmd = m.url.Update(msg) // a URL is often pasted rather than typed
			return m, cmd
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.onKey(msg)
	}
	return m, nil
}

// onKey handles a key press for the step on screen.
func (m *setupModel) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
			m.result = SetupResult{BaseURL: m.found, Model: chosen.ID, CtxSize: chosen.CtxSize, Vision: chosen.Vision}
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg.String() {
	case "esc":
		m.closed = true
		return m, tea.Quit
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
		m.step, m.err = stepProbing, ""
		probe := m.probe
		return m, func() tea.Msg {
			found, models, err := probe(context.Background(), url)
			return probeDoneMsg{baseURL: found, models: models, err: err}
		}
	}
	var cmd tea.Cmd
	m.url, cmd = m.url.Update(msg)
	return m, cmd
}

// the colourless setup styles; the coloured ones are the palette's, rebuilt with it
var (
	setupLabel = lipgloss.NewStyle().Bold(true)
	setupDim   = lipgloss.NewStyle().Faint(true)
)

func (m *setupModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = m.altScreen
	paintTerminal(&v, m.painted)
	return v
}

// render is the box as text, which a session draws over itself and a test reads
func (m *setupModel) render() string {
	var b strings.Builder
	title := "ykode · connect to a model server"
	if m.start.Inside {
		title = "ykode · settings · server and model"
	}
	b.WriteString(setupTitle.Render(title) + "\n\n")
	switch m.step {
	case stepServer, stepProbing:
		b.WriteString(setupDim.Render("ykode talks to an OpenAI-compatible server, such as llama.cpp's\nllama-server (started with --jinja for tool calls).") + "\n\n")
		b.WriteString(field("Server URL", m.url.View()) + "\n\n")
		if m.step == stepProbing {
			b.WriteString(setupDim.Render("asking the server for its models…") + "\n\n")
		} else if m.err != "" {
			b.WriteString(setupError.Render(strings.Join(wrap(m.err, 60), "\n")) + "\n\n")
		}
		leave := "esc quits"
		if m.start.Inside {
			leave = "esc closes"
		}
		b.WriteString(setupDim.Render("enter connects · " + leave))
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

// field is one labelled input, marked as the one a key press goes to.
func field(label, input string) string {
	return setupLabel.Render(label) + "\n" + setupPick.Render("❯ ") + input
}
