package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// settingsScreen is /settings: every knob in one list, written and applied as it is changed
type settingsScreen struct {
	rows       []config.Setting // the knobs after the server row
	at         int              // the row under the cursor; 0 is the server row
	input      textinput.Model  // the value being typed, when editing
	editing    bool
	editingKey string // the knob the value being typed belongs to
	note       string // what the last change did, or why it did not happen
	bad        bool   // the note is a refusal
}

// detailRows is what the pane always takes, so the box keeps its height: thinking needs most
const detailRows = 6

// handled are the knobs the server row stands for: they are not listed twice.
var handled = map[string]bool{"base_url": true, "model": true}

// liveKnobs are the settings a running session takes up at once, from config's one key list
var liveKnobs = config.LiveKnobs()

func newSettingsScreen(cfg config.Config) *settingsScreen {
	in := textinput.New()
	in.Prompt, in.CharLimit = "", 300
	in.SetWidth(40)
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

// key handles a press, saying whether to open the server screen, close, or just redraw
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
		// the knob is the one typing started on, not where the cursor reached when enter came
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

// commit writes one value, checked first, so a refusal says why and leaves the file alone
func (s *settingsScreen) commit(key, value string, write func(key, value string) error) bool {
	if err := write(key, value); err != nil {
		s.note, s.bad = strings.TrimPrefix(err.Error(), "not changed: "), true
		return false
	}
	s.bad = false
	return true
}

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
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, settingsBox.Width(framed(settingsBox, inner)).Render(b.String()))
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
		// the budget table is one styled row cut to the box: a break would leak its colour
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

// openSettings waits for the turn: a running request is using the client, model and options
func (m *model) openSettings() {
	if m.running {
		m.log.notice("settings change between turns: press esc to stop this one first")
		return
	}
	m.screen = newSettingsScreen(m.cfg)
}

// openServer is the first row: the screen a first run starts with, over this one.
func (m *model) openServer() {
	m.modal = newSetup(SetupStart{BaseURL: m.cfg.BaseURL, Model: m.agent.Model(), Inside: true}, m.probe)
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

// writeSetting writes a knob, reloads the settings, and applies what a live session can take
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
	if now.Theme != before.Theme {
		m.useTheme(now.Theme) // the settings screen itself is redrawn in it as the choice lands
	}
	if now.ThinkingLevel != before.ThinkingLevel {
		// only a change to the level is written: saving the old value would undo an unsaved /think
		m.agent.SetThinkingLevel(now.ThinkingLevel) // a turn in flight refuses, and says so itself
	}
	m.agent.Tune(func(o *agent.Options) {
		o.AssumeYes = now.AssumeYes
		o.MaxTurns, o.MaxTokens, o.SafetyMargin = now.MaxTurns, now.MaxTokens, now.SafetyMargin
		// the model cards travel with the run, so a later /model takes the model it switches to
		o.Cards = now.Cards()
		if card, known := o.Cards[m.agent.Model()]; known {
			o.Thinking, o.NoThink = card.Thinking, card.NoThink
		}
	})
}

// budgetLine is every level with its token budget, the selected one marked, as ykode sends them
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

// openSettingsOn opens with the cursor on one setting, rather than printing its value
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
