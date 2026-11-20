package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// modelScreen is a bare /model: the server's list, and switching to the one under the cursor
type modelScreen struct {
	chooser
	models  []llm.ModelInfo
	current string // the model the run is using, so the list says which row that is
	waiting bool   // the server has not answered yet
	use     func(llm.ModelInfo)
}

// modelsFoundMsg is the server's list: the box opens first, as a loading server takes its time
type modelsFoundMsg struct {
	models []llm.ModelInfo
	err    error
}

// openModels puts an empty list on the screen and returns what waits for the server.
func (m *model) openModels() tea.Cmd {
	s := &modelScreen{
		chooser: newChooser("model", "models on this server",
			"enter switches to it · type to narrow · ↑/↓ moves · esc closes"),
		current: m.agent.Model(),
		waiting: true,
	}
	s.use = func(model llm.ModelInfo) {
		m.agent.UseModel(model, true)
		m.log.notice(fmt.Sprintf("model: %s · window %s · the next request asks for it", model.ID, tokCount(m.agent.CtxSize())))
	}
	m.picker = s
	ask, ctx := m.agent.ServerModels, m.ctx
	return func() tea.Msg {
		models, err := ask(ctx)
		return modelsFoundMsg{models: models, err: err}
	}
}

// found fills the list with the server's answer, or says why there is none.
func (s *modelScreen) found(models []llm.ModelInfo, err error) {
	s.waiting = false
	if err != nil {
		s.note = "the server did not list its models: " + err.Error()
		return
	}
	s.models = models
	s.load(s.modelRows())
}

func (s *modelScreen) modelRows() []listRow {
	rows := make([]listRow, len(s.models))
	for i, model := range s.models {
		mark := " "
		if model.ID == s.current {
			mark = "*"
		}
		rows[i] = listRow{
			id:    model.ID,
			mark:  mark,
			text:  model.ID,
			hay:   model.ID,
			cells: []listCell{{text: windowCell(model.CtxSize)}},
		}
	}
	return rows
}

// modelDetail is the cursor's model: its name, the window it brings, and what choosing does
func (s *modelScreen) modelDetail(id string) []string {
	if s.waiting {
		return []string{"asking the server what it offers…"}
	}
	model := s.model(id)
	if model.ID == "" {
		return nil
	}
	lines := []string{model.ID, windowTextOf(model.CtxSize)}
	if model.ID == s.current {
		lines = append(lines, styleWarn.Render("this is the model this run is using"))
		return lines
	}
	return append(lines, "enter switches to it: the next request asks for it, and a model the server has to load waits before the first token")
}

func (s *modelScreen) model(id string) llm.ModelInfo {
	for _, model := range s.models {
		if model.ID == id {
			return model
		}
	}
	return llm.ModelInfo{}
}

// pick switches the run to the chosen model. The server was already asked, so nothing here waits.
func (s *modelScreen) pick(id string) (tea.Cmd, overlayNext) {
	model := s.model(id)
	switch {
	case model.ID == "":
		return nil, stayOpen
	case model.ID == s.current:
		s.note = "this is the model this run is using"
		return nil, stayOpen
	}
	s.use(model)
	return nil, closeOverlay
}

func (s *modelScreen) keys(msg tea.Msg) (tea.Cmd, overlayNext, bool) {
	return s.chooser.keys(msg, s.pick)
}

func (s *modelScreen) view(width, height int) string {
	return s.chooser.view(width, height, s.modelDetail)
}

// windowCell is a model's window as a column: short, because the row has to keep room for the id.
func windowCell(tokens int) string {
	switch {
	case tokens <= 0:
		return padTo("window?", 9)
	case tokens >= 1000:
		return padTo(fmt.Sprintf("%dk window", tokens/1000), 9)
	}
	return padTo(fmt.Sprintf("%d window", tokens), 9)
}

// windowTextOf is the same window spelled out, where the pane has room for it.
func windowTextOf(tokens int) string {
	if tokens <= 0 {
		return "window unknown: the run's budget is what counts"
	}
	return fmt.Sprintf("%s token window", tokCount(tokens))
}
