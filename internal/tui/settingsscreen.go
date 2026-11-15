package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// settingsScreen is /settings: every knob settings.yaml can set, in one list. The first row is the server
// and model, which opens the screen a first run starts with; the rest are edited where they stand. A
// change is checked and written the moment it is made, and applied to the running session where that is
// safe, so the screen never holds a change that has not happened.
type settingsScreen struct {
	rows       []config.Setting // the knobs after the server row
	at         int              // the row under the cursor; 0 is the server row
	input      textinput.Model  // the value being typed, when editing
	editing    bool
	editingKey string // the knob the value being typed belongs to
	note       string // what the last change did, or why it did not happen
	bad        bool   // the note is a refusal
}

// detailRows is what the detail under the list always takes, so the box does not change height as the
// cursor moves: the thinking level needs the most, with its table of budgets.
const detailRows = 6

// handled are the knobs the server row stands for: they are not listed twice.
var handled = map[string]bool{"base_url": true, "model": true, "api_key": true}

// liveKnobs are the settings a running session takes up at once; the others are read when ykode starts.
var liveKnobs = map[string]bool{
	"thinking_level": true, "assume_yes": true, "mouse": true,
	"max_turns": true, "max_tokens": true, "context.safety_margin": true,
}

func newSettingsScreen(cfg config.Config) *settingsScreen {
	in := textinput.New()
	in.Prompt, in.CharLimit, in.Width = "", 300, 40
	s := &settingsScreen{input: in}
	s.load(cfg)
	return s
}

// load reads the resolved settings again, for the rows and for the value shown beside each.
func (s *settingsScreen) load(cfg config.Config) {
	s.rows = s.rows[:0]
	for _, set := range cfg.Catalogue() {
		if !handled[set.Key] {
			s.rows = append(s.rows, set)
		}
	}
}

// current is the knob under the cursor; the server row has none.
func (s *settingsScreen) current() (config.Setting, bool) {
	if s.at == 0 {
		return config.Setting{}, false
	}
	return s.rows[s.at-1], true
}

// key handles one key press. It returns what the session should do next: open the server screen, close
// this one, or just redraw.
func (s *settingsScreen) key(msg tea.KeyMsg, write func(key, value string) error) (action settingsAction, cmd tea.Cmd) {
	if s.editing {
		return s.typing(msg, write)
	}
	set, onKnob := s.current()
	switch msg.String() {
	case "esc", "q", "ctrl+c":
		return actClose, nil
	case "up", "k":
		s.at = (s.at + len(s.rows)) % (len(s.rows) + 1)
	case "down", "j":
		s.at = (s.at + 1) % (len(s.rows) + 1)
	case "pgup":
		s.at = max(s.at-8, 0)
	case "pgdown":
		s.at = min(s.at+8, len(s.rows))
	case "home":
		s.at = 0
	case "end":
		s.at = len(s.rows)
	case "left", "h":
		if onKnob && len(set.Options) > 0 {
			s.cycle(set, -1, write)
		}
	case "enter", " ", "right", "l":
		switch {
		case !onKnob:
			return actServer, nil
		case set.Switch:
			next := "on"
			if set.Value == "on" {
				next = "off"
			}
			s.commit(set.Key, next, write)
		case len(set.Options) > 0:
			s.cycle(set, 1, write)
		case msg.String() == " " || msg.String() == "right" || msg.String() == "l":
			// space and the arrows only turn switches and choices; a typed value takes enter
		default:
			s.begin(set)
			return actNone, textinput.Blink
		}
	}
	return actNone, nil
}

type settingsAction int

const (
	actNone settingsAction = iota
	actClose
	actServer
)

// begin starts typing a value: a list asks for a new entry, anything else edits what is there.
func (s *settingsScreen) begin(set config.Setting) {
	s.editing, s.editingKey, s.note, s.bad = true, set.Key, "", false
	s.input.SetValue("")
	if !set.List && set.Value != "" {
		s.input.SetValue(set.Value)
	}
	s.input.CursorEnd()
	s.input.Focus()
}

func (s *settingsScreen) typing(msg tea.KeyMsg, write func(key, value string) error) (settingsAction, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		s.editing, s.editingKey = false, ""
		s.input.Blur()
		return actNone, nil
	case "enter":
		// the key the typing started on, not whichever row the cursor reaches by then: a value written
		// against the wrong knob is checked against that knob's kind, so a number meant for one setting
		// is accepted by any other setting of the same kind and saved
		if s.commit(s.editingKey, strings.TrimSpace(s.input.Value()), write) {
			s.editing, s.editingKey = false, ""
			s.input.Blur()
		}
		return actNone, nil
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return actNone, cmd
}

// cycle moves a choice to the next or previous of its options.
func (s *settingsScreen) cycle(set config.Setting, by int, write func(key, value string) error) {
	at := slices.Index(set.Options, set.Value)
	next := set.Options[((at+by)%len(set.Options)+len(set.Options))%len(set.Options)]
	s.commit(set.Key, next, write)
}

// commit writes one value. The write checks it first, so a refusal leaves the file as it was and says
// why in place; true means it was written.
func (s *settingsScreen) commit(key, value string, write func(key, value string) error) bool {
	if err := write(key, value); err != nil {
		s.note, s.bad = strings.TrimPrefix(err.Error(), "not changed: "), true
		return false
	}
	s.bad = false
	return true
}

var (
	settingsBox  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Padding(0, 2)
	settingsKey  = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	settingsLive = lipgloss.NewStyle().Foreground(colorOK)
)

// view draws the screen within width and height columns and rows of terminal.
func (s *settingsScreen) view(width, height int, cfg config.Config, model string) string {
	inner := min(max(width-8, 40), 100)
	keyW := 0
	for _, set := range s.rows {
		keyW = max(keyW, len(set.Key))
	}
	// the title, a blank, the server row, the detail (four rows), the hint, and the box's own edges
	where, local := config.SettingsTarget(cfg.Root)
	head, said := setupDim.Render("   this project · .ykode/settings.yaml"), "this project · .ykode/settings.yaml"
	if !local {
		said = "⚠ global · " + shortenHome(where) + " — every project is changed · /create-local-setting makes one for this project"
		head = styleWarn.Render("   " + said)
	}
	room := max(height-detailRows-10-(len(wrap(said, inner))-1), 4)
	first := min(max(s.at-room/2, 0), max(len(s.rows)+1-room, 0))

	var b strings.Builder
	b.WriteString(setupTitle.Render("ykode · settings") + head + "\n\n")
	for i := first; i < min(first+room, len(s.rows)+1); i++ {
		b.WriteString(s.row(i, keyW, inner, cfg, model) + "\n")
	}
	b.WriteString("\n" + s.detail(inner))
	b.WriteString("\n" + setupDim.Render(s.hint()))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, settingsBox.Width(inner).Render(b.String()))
}

func (s *settingsScreen) row(i, keyW, inner int, cfg config.Config, model string) string {
	mark := "  "
	if i == s.at {
		mark = setupPick.Render("❯ ")
	}
	if i == 0 {
		what := cutToWidth(fmt.Sprintf("%s · %s", orUnset(model), cfg.BaseURL), max(inner-4-2-keyW-2, 10))
		return mark + settingsKey.Render(fmt.Sprintf("%-*s", keyW, "server & model")) + "  " + what
	}
	set := s.rows[i-1]
	value := orUnset(set.Value)
	if s.editing && i == s.at {
		value = s.input.View()
	} else {
		room := inner - 4 - 2 - keyW - 2 // the box's padding, the mark, and the gap after the key
		if !liveKnobs[set.Key] {
			room -= len("  next run")
		}
		value = cutToWidth(value, max(room, 8))
		if set.From == config.FromDefault {
			value = styleDim.Render(value)
		}
	}
	tag := ""
	if !liveKnobs[set.Key] {
		tag = setupDim.Render("  next run")
	}
	return mark + fmt.Sprintf("%-*s", keyW, set.Key) + "  " + value + tag
}

// detail explains the knob under the cursor, or the last thing that happened.
func (s *settingsScreen) detail(inner int) string {
	lines := make([]string, 0, 4)
	budgets, level := false, ""
	switch set, ok := s.current(); {
	case !ok:
		lines = append(lines, "the server, its key and the model: connect, pick, and the session switches to it")
	default:
		lines = append(lines, set.Note)
		facts := fmt.Sprintf("default %s · from %s", orUnset(set.Default), set.From)
		if len(set.Options) > 0 {
			facts = "choices " + strings.Join(set.Options, " ") + " · " + facts
		}
		if liveKnobs[set.Key] {
			facts += " · " + settingsLive.Render("applies now")
		} else {
			facts += " · read when ykode starts"
		}
		lines = append(lines, setupDim.Render(facts))
		budgets, level = set.Key == "thinking_level", set.Value
		if set.From == config.FromEnv || set.From == config.FromFlag {
			lines = append(lines, styleWarn.Render("a "+set.From+" sets this for the run: the file is changed, the "+set.From+" still wins"))
		}
	}
	wrapped := make([]string, 0, detailRows)
	for _, l := range lines {
		wrapped = append(wrapped, wrap(l, inner-4)...)
	}
	if budgets {
		// the table of budgets is one styled row and is cut to the box, never wrapped: a break inside
		// the marked level would leave its colour running into the border
		wrapped = append(wrapped, cutToWidth(budgetLine(level), inner-4))
		for _, row := range wrap(budgetNote(level), inner-4) {
			wrapped = append(wrapped, setupDim.Render(row))
		}
	}
	for len(wrapped) < detailRows {
		wrapped = append(wrapped, "")
	}
	out := strings.Join(wrapped[:min(len(wrapped), detailRows)], "\n")
	if s.note != "" {
		style := styleOK
		if s.bad {
			style = styleBad
		}
		out += "\n" + style.Render(cutToWidth(s.note, inner))
	} else {
		out += "\n"
	}
	return out
}

func (s *settingsScreen) hint() string {
	set, ok := s.current()
	switch {
	case s.editing && set.List:
		return "enter adds the entry · esc cancels"
	case s.editing:
		return "enter saves · esc cancels"
	case !ok:
		return "enter opens it · ↑/↓ moves · esc closes"
	case set.Switch:
		return "enter or space turns it on or off · ↑/↓ moves · esc closes"
	case len(set.Options) > 0:
		return "←/→ or enter picks the next choice · ↑/↓ moves · esc closes"
	case set.List:
		return "enter adds an entry · ↑/↓ moves · esc closes"
	}
	return "enter edits the value · ↑/↓ moves · esc closes"
}

// ---- the session's side of the screen ------------------------------------------------------------

// openSettings puts the settings screen over the session. It waits for the turn to finish: the client,
// the model and the options are what a request in flight is using.
func (m *model) openSettings() {
	if m.running {
		m.log.notice("settings change between turns: press esc to stop this one first")
		return
	}
	m.screen = newSettingsScreen(m.cfg)
}

// openServer is the first row: the screen a first run starts with, over this one.
func (m *model) openServer() {
	m.modal = newSetup(SetupStart{BaseURL: m.cfg.BaseURL, APIKey: m.cfg.APIKey, Model: m.agent.Model(), Inside: true}, m.probe)
	m.modal.width = m.width
}

// screenMsg gives the settings screen its keys. The server screen, when it is open, has them first.
func (m *model) screenMsg(msg tea.Msg) (tea.Cmd, bool) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false
	}
	action, cmd := m.screen.key(key, m.writeSetting)
	switch action {
	case actClose:
		m.screen = nil
		m.refresh()
	case actServer:
		m.openServer()
	}
	return cmd, true
}

// writeSetting writes one knob to the settings file this project uses, reads the settings again, and
// applies what a running session can take up. A value the settings package refuses never reaches the
// file.
func (m *model) writeSetting(key, value string) error {
	before := m.cfg
	path, local, err := config.Set(m.cfg.Root, key, value)
	if err != nil {
		return err
	}
	fresh, err := m.cfg.Reload()
	if err != nil {
		return err
	}
	m.cfg = fresh
	m.screen.load(fresh)
	m.screen.note = savedTo(key, path, local)
	if liveKnobs[key] {
		m.applyLive(before, fresh)
	}
	return nil
}

// applyLive hands the session what the change moved. Only the knobs in liveKnobs are here.
func (m *model) applyLive(before, now config.Config) {
	if now.ThinkingLevel != before.ThinkingLevel {
		// only a change to the level itself: writing the saved value back would undo a /think the session
		// made without saving it
		m.agent.SetThinkingLevel(now.ThinkingLevel) // a turn in flight refuses, and says so itself
	}
	m.agent.Tune(func(o *agent.Options) {
		o.AssumeYes = now.AssumeYes
		o.MaxTurns, o.MaxTokens, o.SafetyMargin = now.MaxTurns, now.MaxTokens, now.SafetyMargin
	})
	if now.Mouse != before.Mouse {
		m.mouseOn = now.Mouse
		m.wantMouse = true // the next update tells the terminal
	}
}

// budgetLine is every thinking level with the tokens it lets the model think for, the selected one marked.
// The numbers are what ykode sends the server, so moving between levels shows what each one costs.
func budgetLine(selected string) string {
	parts := make([]string, 0, 7)
	for _, level := range llm.ThinkingLevels() {
		text := strings.TrimSpace(level + " " + budgetText(level))
		if level == selected {
			text = settingsKey.Render("[" + text + "]")
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, " · ")
}

// budgetText is a level's thinking budget in words; off has none to give and max has no cap.
func budgetText(level string) string {
	budget, _ := llm.ThinkingBudgetTokens(level)
	switch {
	case level == llm.ThinkingOff:
		return "" // nothing to think for: the level's name says it
	case budget <= 0:
		return "no cap"
	}
	return groupDigits(budget)
}

// budgetNote says what the selected level's budget does to the reply.
func budgetNote(level string) string {
	switch budget, _ := llm.ThinkingBudgetTokens(level); {
	case level == llm.ThinkingOff:
		return "the model answers straight away, with no thinking phase"
	case budget <= 0:
		return "no cap on thinking: it shares the whole reply with the answer, so a long thought can leave none"
	default:
		return fmt.Sprintf("thinking stops at %s tokens and the answer takes the rest of the reply", groupDigits(budget))
	}
}

// groupDigits writes 16384 as 16,384.
func groupDigits(n int) string {
	text := fmt.Sprint(n)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}

// openSettingsOn opens the settings screen with the cursor on one setting, which is what a command about
// that setting should do rather than print a line saying what its current value is.
func (m *model) openSettingsOn(key string) {
	m.openSettings()
	if m.screen == nil {
		return
	}
	for i, set := range m.screen.rows {
		if set.Key == key {
			m.screen.at = i + 1 // the first row is the server and model, above the settings themselves
			return
		}
	}
}
